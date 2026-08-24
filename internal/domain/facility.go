package domain

// Facility is a bookable municipal space. Cost is in cents to avoid float
// rounding; a facility with FeeCents == 0 is free. ParentID lets a space
// belong to a building (e.g. a room within a community centre).
type Facility struct {
	Base
	Name                string `gorm:"type:varchar(200);index" json:"name"`
	Description         string `gorm:"type:text" json:"description"`
	Capacity            int    `json:"capacity"`
	FeeCents            int    `json:"feeCents"`            // per-booking fee for residents; 0 = free
	NonResidentFeeCents int    `json:"nonResidentFeeCents"` // fee for non-residents; 0 = same as FeeCents
	DepositCents        int    `json:"depositCents"`        // refundable hold; 0 = none
	Location            string `gorm:"type:varchar(300)" json:"location"`
	// Area is the neighbourhood/zone used by the §4.3 area filter. Location is a
	// free-text street address, which cannot be filtered on meaningfully — a
	// resident looking for "somewhere in the north end" is not going to type a
	// postal address. Kept separate rather than parsed out of Location so staff
	// control the vocabulary the filter offers.
	Area      string  `gorm:"type:varchar(120);index" json:"area"`
	ImageURL  string  `gorm:"type:varchar(500)" json:"imageUrl"`
	Latitude  float64 `json:"latitude"` // for the map view (§4.11); 0 = not placed
	Longitude float64 `json:"longitude"`

	ParentID *string   `gorm:"type:varchar(36);index" json:"parentId,omitempty"`
	Parent   *Facility `gorm:"foreignKey:ParentID" json:"-"`

	// Booking rules.
	RequiresApproval bool `json:"requiresApproval"` // else auto-confirm
	RequiresWaiver   bool `json:"requiresWaiver"`   // a signed waiver/insurance doc is needed before confirmation (§4.11)
	MinMinutes       int  `json:"minMinutes"`       // minimum booking length
	MaxMinutes       int  `json:"maxMinutes"`       // maximum booking length
	BufferMinutes    int  `json:"bufferMinutes"`    // setup/cleanup gap enforced between bookings

	// Accessibility details, shown on the facility page.
	StepFreeAccess     bool `json:"stepFreeAccess"`
	AccessibleWashroom bool `json:"accessibleWashroom"`

	BeforeInstructions string `gorm:"type:text" json:"beforeInstructions"`
	AfterInstructions  string `gorm:"type:text" json:"afterInstructions"`

	Accessories []FacilityAccessory `gorm:"foreignKey:FacilityID" json:"accessories,omitempty"`
}

// FeeFor returns the booking fee for a resident vs non-resident. A facility with
// no non-resident fee set charges everyone the base fee.
func (f Facility) FeeFor(isResident bool) int {
	if !isResident && f.NonResidentFeeCents > 0 {
		return f.NonResidentFeeCents
	}
	return f.FeeCents
}

// Accessory is a reusable item that facilities can offer (projector, chairs…).
type Accessory struct {
	Base
	Name string `gorm:"type:varchar(120);uniqueIndex" json:"name"`
}

// FacilityAccessory is the join row: which accessory a facility offers, and how
// many are available in that space.
type FacilityAccessory struct {
	Base
	FacilityID  string    `gorm:"type:varchar(36);index" json:"facilityId"`
	AccessoryID string    `gorm:"type:varchar(36);index" json:"accessoryId"`
	Accessory   Accessory `gorm:"foreignKey:AccessoryID" json:"accessory"`
	Quantity    int       `json:"quantity"`
}
