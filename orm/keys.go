package orm

import (
	"crypto/rand"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// UUIDKey is an embeddable primary key that fills itself with a time-ordered UUID (version 7) on insert
// (Eloquent's HasUuids). It is the bare key: embed UUIDModel instead to also get the timestamps and
// the instance methods (Save, Delete, ...). A BeforeCreate hook on the model
// itself replaces this one.
type UUIDKey struct {
	ID string `gorm:"primaryKey;size:36"`
}

func (k *UUIDKey) BeforeCreate(*gorm.DB) error {
	if k.ID == "" {
		k.ID = NewUUID()
	}
	return nil
}

// ULIDKey is UUIDKey with a 26-character ULID (Eloquent's HasUlids).
type ULIDKey struct {
	ID string `gorm:"primaryKey;size:26"`
}

func (k *ULIDKey) BeforeCreate(*gorm.DB) error {
	if k.ID == "" {
		k.ID = NewULID()
	}
	return nil
}

func randomBytes(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("migro: system randomness unavailable: " + err.Error()) // crypto/rand does not fail on supported platforms
	}
}

// NewUUID returns a UUIDv7: a millisecond timestamp followed by random bits, so ids sort by creation.
func NewUUID() string {
	var b [16]byte
	randomBytes(b[:])
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a ULID: 48 bits of millisecond time and 80 random bits in Crockford base32.
func NewULID() string {
	var b [16]byte
	randomBytes(b[6:])
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	var hi, lo uint64
	for i := range 8 {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[8+i])
	}
	var out [26]byte
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}
