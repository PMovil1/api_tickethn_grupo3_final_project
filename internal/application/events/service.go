package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
)

var (
	ErrStorage  = errors.New("almacenamiento de afiches no disponible")
	ErrQR       = errors.New("firma QR no configurada")
	uuidPattern = regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$")
)

type Service struct {
	repo    Repository
	storage CoverStore
	qr      QRSigner
	logger  *slog.Logger
}

func New(repo Repository, storage CoverStore, qr QRSigner, logger *slog.Logger) *Service {
	return &Service{repo: repo, storage: storage, qr: qr, logger: logger}
}

func ValidID(id string) bool {
	return uuidPattern.MatchString(id)
}

func (s *Service) OwnedEvents(ctx context.Context, owner string, page Page) ([]domain.Event, error) {
	return s.repo.OwnedEvents(ctx, owner, page)
}

func (s *Service) OwnedEvent(ctx context.Context, owner, id string) (domain.Event, error) {
	return s.repo.OwnedEvent(ctx, owner, id)
}

func (s *Service) PublicEvents(ctx context.Context, page Page) ([]domain.Event, error) {
	return s.repo.PublicEvents(ctx, page)
}

func (s *Service) PublicEvent(ctx context.Context, id string) (domain.Event, error) {
	return s.repo.PublicEvent(ctx, id)
}

func (s *Service) Orders(ctx context.Context, buyer string, page Page) ([]domain.Order, error) {
	return s.repo.Orders(ctx, buyer, page)
}

func (s *Service) Tickets(ctx context.Context, buyer string, page Page) ([]domain.Ticket, error) {
	return s.repo.Tickets(ctx, buyer, page)
}

func (s *Service) Ticket(ctx context.Context, buyer, id string) (domain.Ticket, string, error) {
	ticket, err := s.repo.Ticket(ctx, buyer, id)
	if err != nil {
		return ticket, "", err
	}
	if s.qr == nil {
		return ticket, "", ErrQR
	}
	return ticket, s.qr.Sign(ticket.ID, ticket.EventID), nil
}

func (s *Service) Create(ctx context.Context, owner string, details domain.EventDetails, cover *Cover) (domain.Event, error) {
	if err := details.Validate(); err != nil {
		return domain.Event{}, err
	}
	event := domain.Event{ID: randomID(), OrganizerID: owner, EventDetails: details, Status: domain.Draft}
	if cover == nil {
		return s.repo.Create(ctx, event)
	}
	url, err := s.upload(ctx, *cover)
	if err != nil {
		return domain.Event{}, err
	}
	event.CoverURL = &url
	saved, err := s.repo.Create(ctx, event)
	if err != nil && s.reconcile(ctx, event.ID, url) && saved.ID != "" {
		return saved, nil
	}
	return saved, err
}

func (s *Service) ReplaceCover(ctx context.Context, owner, id string, cover Cover) (domain.Event, error) {
	if _, err := s.repo.OwnedEvent(ctx, owner, id); err != nil {
		return domain.Event{}, err
	}
	url, err := s.upload(ctx, cover)
	if err != nil {
		return domain.Event{}, err
	}
	var saved domain.Event
	var previous *string
	err = s.withOwned(ctx, owner, id, func(tx EventTransaction, event domain.Event) error {
		previous = event.CoverURL
		var err error
		saved, err = tx.SetCover(url)
		return err
	})
	if err != nil {
		if !s.reconcile(ctx, id, url) || saved.ID == "" {
			return domain.Event{}, err
		}
	}
	if previous != nil {
		s.removeUnreferenced(ctx, *previous)
	}
	return saved, nil
}

func (s *Service) upload(ctx context.Context, cover Cover) (string, error) {
	if s.storage == nil {
		return "", ErrStorage
	}
	key := randomID() + "." + cover.Extension
	url, err := s.storage.Upload(ctx, key, cover)
	if err != nil {
		s.logger.WarnContext(ctx, "Cover upload failed", "object", key, "error", err)
		return "", ErrStorage
	}
	return url, nil
}

func randomID() string {
	var value [16]byte
	rand.Read(value[:])
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	id := hex.EncodeToString(value[:])
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

// reconcile waits for the save attempt to settle before removing an uploaded object.
func (s *Service) reconcile(ctx context.Context, id, url string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	saved, err := s.repo.ConfirmCover(ctx, id, url)
	if err != nil {
		s.logger.WarnContext(ctx, "Cover reference could not be verified", "url", url, "error", err)
		return false
	}
	if !saved {
		s.removeUnreferenced(ctx, url)
	}
	return saved
}

func (s *Service) removeUnreferenced(ctx context.Context, url string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	referenced, err := s.repo.CoverReferenced(ctx, url)
	if err == nil && !referenced {
		err = s.storage.Delete(ctx, url)
	}
	if err != nil {
		s.logger.WarnContext(ctx, "Cover cleanup pending", "url", url, "error", err)
	}
}

func (s *Service) withOwned(ctx context.Context, owner, id string, action func(EventTransaction, domain.Event) error) error {
	return s.repo.WithEvent(ctx, id, func(tx EventTransaction, event domain.Event) error {
		if event.OrganizerID != owner {
			return domain.ErrEventNotFound
		}
		return action(tx, event)
	})
}

func (s *Service) Edit(ctx context.Context, owner, id string, apply func(*domain.EventDetails)) (domain.Event, error) {
	var saved domain.Event
	err := s.withOwned(ctx, owner, id, func(tx EventTransaction, event domain.Event) error {
		next := event.EventDetails
		apply(&next)
		if err := next.Validate(); err != nil {
			return err
		}
		if event.EventDetails.ChangesScheduleOrLocation(next) {
			issued, err := tx.HasTickets()
			if err != nil {
				return err
			}
			if issued {
				return domain.ErrEventConflict
			}
		}
		if event.Status == domain.Published && (!next.Ready() ||
			((event.StartsAt == nil || !event.StartsAt.Equal(*next.StartsAt)) && !next.StartsAt.After(time.Now()))) {
			return domain.ErrEventConflict
		}
		event.EventDetails = next
		var err error
		saved, err = tx.UpdateEvent(event)
		return err
	})
	return saved, err
}

func (s *Service) Publish(ctx context.Context, owner, id string) (domain.Event, error) {
	var saved domain.Event
	err := s.withOwned(ctx, owner, id, func(tx EventTransaction, event domain.Event) error {
		if event.Status != domain.Draft || !event.Ready() || !event.StartsAt.After(time.Now()) {
			return domain.ErrEventConflict
		}
		active, err := tx.HasOtherActiveType("")
		if err != nil {
			return err
		}
		if !active {
			return domain.ErrEventConflict
		}
		event.Status = domain.Published
		saved, err = tx.UpdateEvent(event)
		return err
	})
	return saved, err
}

func (s *Service) CreateTicketType(ctx context.Context, owner, id string, input domain.TicketTypeDetails) (domain.TicketType, error) {
	var saved domain.TicketType
	err := s.withOwned(ctx, owner, id, func(tx EventTransaction, _ domain.Event) error {
		if err := input.Validate(); err != nil {
			return err
		}
		var err error
		saved, err = tx.CreateTicketType(input)
		return err
	})
	return saved, err
}

func (s *Service) EditTicketType(ctx context.Context, owner, id, typeID string, apply func(*domain.TicketTypeDetails)) (domain.TicketType, error) {
	var saved domain.TicketType
	err := s.withOwned(ctx, owner, id, func(tx EventTransaction, event domain.Event) error {
		locality, err := tx.TicketType(typeID)
		if err != nil {
			return err
		}
		wasActive := locality.Active
		apply(&locality.TicketTypeDetails)
		if err := locality.TicketTypeDetails.Validate(); err != nil {
			return err
		}
		if locality.Capacity < locality.SoldCount {
			return domain.ErrEventConflict
		}
		if event.Status == domain.Published && wasActive && !locality.Active {
			other, err := tx.HasOtherActiveType(typeID)
			if err != nil {
				return err
			}
			if !other {
				return domain.ErrEventConflict
			}
		}
		saved, err = tx.UpdateTicketType(locality)
		return err
	})
	return saved, err
}

func (s *Service) Buy(ctx context.Context, buyer, id string, items []domain.OrderItem) (domain.Order, error) {
	if !ValidID(id) || len(items) == 0 || len(items) > domain.MaxTicketsPerOrder {
		return domain.Order{}, domain.ErrEventInput
	}
	items = slices.Clone(items)
	seen := make(map[string]bool, len(items))
	count := 0
	for i, item := range items {
		key := strings.ToLower(item.TicketTypeID)
		if !ValidID(key) || item.Quantity < 1 || item.Quantity > domain.MaxTicketsPerOrder-count || seen[key] {
			return domain.Order{}, domain.ErrEventInput
		}
		items[i].TicketTypeID = key
		seen[key] = true
		count += item.Quantity
	}
	slices.SortFunc(items, func(a, b domain.OrderItem) int { return strings.Compare(a.TicketTypeID, b.TicketTypeID) })
	var order domain.Order
	err := s.repo.WithEvent(ctx, id, func(tx EventTransaction, event domain.Event) error {
		if event.Status != domain.Published || event.StartsAt == nil || !event.StartsAt.After(time.Now()) {
			return domain.ErrEventConflict
		}
		var total int64
		prices := make([]int64, len(items))
		for i, item := range items {
			price, err := tx.Reserve(item.TicketTypeID, item.Quantity)
			if err != nil {
				return err
			}
			prices[i] = price
			total += price * int64(item.Quantity)
		}
		var err error
		order, err = tx.CreateOrder(buyer, total)
		if err != nil {
			return err
		}
		order.Tickets = make([]domain.Ticket, 0, count)
		for i, item := range items {
			tickets, err := tx.IssueTickets(order.ID, item.TicketTypeID, prices[i], item.Quantity)
			if err != nil {
				return fmt.Errorf("issue tickets: %w", err)
			}
			order.Tickets = append(order.Tickets, tickets...)
		}
		return nil
	})
	return order, err
}
