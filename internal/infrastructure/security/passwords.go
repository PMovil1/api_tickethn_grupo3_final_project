package security

import "golang.org/x/crypto/bcrypt"

type Bcrypt struct {
	dummyHash string
}

func NewBcrypt() (*Bcrypt, error) {
	passwords := &Bcrypt{}
	dummy, err := passwords.Hash("TicketHN dummy password")
	if err != nil {
		return nil, err
	}
	passwords.dummyHash = dummy
	return passwords, nil
}

func (*Bcrypt) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func (*Bcrypt) Matches(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (b *Bcrypt) DummyHash() string {
	return b.dummyHash
}
