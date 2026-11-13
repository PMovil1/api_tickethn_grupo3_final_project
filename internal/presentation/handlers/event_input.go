package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	_ "golang.org/x/image/webp"
)

const (
	maxCoverBytes     = 5 * 1024 * 1024
	maxMultipartBytes = maxCoverBytes + 64*1024
	maxFieldBytes     = 8192
)

var (
	errLargeBody   = errors.New("la petición o el afiche excede el límite de tamaño")
	errCoverFormat = errors.New("el afiche debe ser JPEG, PNG o WebP válido")
	zoneDate       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)
	pricePattern   = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$`)
)

type field[T any] struct {
	present bool
	value   *T
}

func (f *field[T]) UnmarshalJSON(data []byte) error {
	f.present = true
	return json.Unmarshal(data, &f.value)
}

func (f field[T]) apply(target **T) {
	if f.present {
		*target = f.value
	}
}

type eventInput struct {
	Title       field[string]  `json:"title"`
	Description field[string]  `json:"description"`
	Address     field[string]  `json:"address"`
	Latitude    field[float64] `json:"latitude"`
	Longitude   field[float64] `json:"longitude"`
	StartsAt    field[string]  `json:"starts_at"`
}

func (in eventInput) patch() (func(*domain.EventDetails), error) {
	if (in.Title.present && in.Title.value == nil) || in.Latitude.present != in.Longitude.present {
		return nil, domain.ErrEventInput
	}
	var date *time.Time
	if in.StartsAt.value != nil {
		value := *in.StartsAt.value
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || !zoneDate.MatchString(value) || t.Year() < 1 {
			return nil, domain.ErrEventInput
		}
		date = &t
	}
	return func(d *domain.EventDetails) {
		if in.Title.value != nil {
			d.Title = *in.Title.value
		}
		in.Description.apply(&d.Description)
		in.Address.apply(&d.Address)
		in.Latitude.apply(&d.Latitude)
		in.Longitude.apply(&d.Longitude)
		if in.StartsAt.present {
			d.StartsAt = date
		}
	}, nil
}

type cents int64

func (c *cents) UnmarshalJSON(data []byte) error {
	value := string(data)
	if !pricePattern.MatchString(value) {
		return domain.ErrEventInput
	}
	whole, fraction, _ := strings.Cut(value, ".")
	n, err := strconv.ParseInt(whole+fraction+strings.Repeat("0", 2-len(fraction)), 10, 64)
	*c = cents(n)
	return err
}

func (c cents) MarshalJSON() ([]byte, error) {
	n := int64(c)
	fraction := strconv.FormatInt(n%100+100, 10)[1:]
	return []byte(strconv.FormatInt(n/100, 10) + "." + fraction), nil
}

type localityInput struct {
	Title       field[string] `json:"title"`
	Description field[string] `json:"description"`
	Price       field[cents]  `json:"price"`
	Capacity    field[int]    `json:"capacity"`
	Active      field[bool]   `json:"is_active"`
}

func (in localityInput) patch(create bool) (func(*domain.TicketTypeDetails), error) {
	if ((create || in.Title.present) && in.Title.value == nil) ||
		((create || in.Price.present) && in.Price.value == nil) ||
		((create || in.Capacity.present) && in.Capacity.value == nil) ||
		(in.Active.present && in.Active.value == nil) {
		return nil, domain.ErrEventInput
	}
	return func(d *domain.TicketTypeDetails) {
		if in.Title.value != nil {
			d.Title = *in.Title.value
		}
		in.Description.apply(&d.Description)
		if in.Price.value != nil {
			d.PriceCents = int64(*in.Price.value)
		}
		if in.Capacity.value != nil {
			d.Capacity = *in.Capacity.value
		}
		if in.Active.value != nil {
			d.Active = *in.Active.value
		}
	}, nil
}

func readEventJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxFieldBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return inputError(err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return inputError(err)
	}
	return nil
}

func inputError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errLargeBody
	}
	return domain.ErrEventInput
}

func multipartEvent(w http.ResponseWriter, r *http.Request, coverOnly bool) (eventInput, *events.Cover, error) {
	var in eventInput
	var cover *events.Cover
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return in, nil, domain.ErrEventInput
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return in, nil, inputError(err)
	}
	seen := make(map[string]bool, 7)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return in, nil, inputError(err)
		}
		name := part.FormName()
		if name == "" || seen[name] || (coverOnly && name != "cover") || (name != "cover" && part.FileName() != "") {
			return in, nil, domain.ErrEventInput
		}
		seen[name] = true
		limit := maxFieldBytes
		if name == "cover" {
			limit = maxCoverBytes
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(limit)+1))
		if err != nil {
			return in, nil, inputError(err)
		}
		if len(data) > limit {
			return in, nil, errLargeBody
		}
		if name == "cover" {
			cover, err = validateCover(data)
		} else {
			err = in.setPart(name, string(data))
		}
		if err != nil {
			return in, nil, err
		}
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return in, nil, inputError(err)
	}
	if coverOnly && cover == nil {
		return in, nil, domain.ErrEventInput
	}
	return in, cover, nil
}

func (in *eventInput) setPart(name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case "title":
		in.Title = field[string]{true, &value}
	case "description":
		in.Description = field[string]{true, &value}
	case "address":
		in.Address = field[string]{true, &value}
	case "starts_at":
		in.StartsAt.present = true
		if value != "" {
			in.StartsAt.value = &value
		}
	case "latitude", "longitude":
		f := &in.Latitude
		if name == "longitude" {
			f = &in.Longitude
		}
		f.present = true
		if value != "" {
			number, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return domain.ErrEventInput
			}
			f.value = &number
		}
	default:
		return domain.ErrEventInput
	}
	return nil
}

func validateCover(data []byte) (*events.Cover, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 ||
		cfg.Height > 4096 || cfg.Width*cfg.Height > 12000000 {
		return nil, errCoverFormat
	}
	extension := format
	switch format {
	case "jpeg":
		extension = "jpg"
	case "png", "webp":
	default:
		return nil, errCoverFormat
	}
	img, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format || img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		return nil, errCoverFormat
	}
	return &events.Cover{Bytes: data, Extension: extension, MediaType: "image/" + format}, nil
}
