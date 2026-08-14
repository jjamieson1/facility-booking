package domain

// WaiverDocument is an uploaded waiver / proof-of-insurance for a booking
// (§4.11). Only metadata lives in the DB; the bytes are stored on disk OUTSIDE
// any web-server document root under a randomized StoredName, and are served
// only by streaming through the app (see internal/media).
type WaiverDocument struct {
	Base
	BookingID   string `gorm:"type:varchar(36);uniqueIndex" json:"bookingId"`
	StoredName  string `gorm:"type:varchar(120)" json:"-"`          // random on-disk filename
	ContentType string `gorm:"type:varchar(80)" json:"contentType"` // sniffed from bytes, not the client
	SizeBytes   int64  `json:"sizeBytes"`
}
