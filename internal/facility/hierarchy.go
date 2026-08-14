package facility

import (
	"sort"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// maxHierarchyDepth bounds the walk in both directions. A facility that is its
// own ancestor (bad data, a cycle introduced by an edit) would otherwise loop
// forever inside a transaction holding row locks.
const maxHierarchyDepth = 10

// ConflictSet returns the facility ids whose bookings conflict with a booking on
// facilityID: the facility itself, its ancestors, and its descendants.
//
// A hall and its halves are the same physical space, so a booking on any of them
// occupies all of them. Siblings are *not* included — two halves of a hall are
// genuinely separate spaces and stay independently bookable.
//
// The result is sorted, which matters: callers take `SELECT … FOR UPDATE` locks
// over this set, and a consistent order across concurrent transactions is what
// stops two requests on overlapping subtrees from deadlocking.
func ConflictSet(tx *gorm.DB, facilityID string) ([]string, error) {
	ids := map[string]bool{facilityID: true}

	// Up: parent, grandparent, …
	current := facilityID
	for i := 0; i < maxHierarchyDepth; i++ {
		var rows []domain.Facility
		if err := tx.Select("id", "parent_id").Find(&rows, "id = ?", current).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 || rows[0].ParentID == nil || *rows[0].ParentID == "" {
			break
		}
		parent := *rows[0].ParentID
		if ids[parent] {
			break // cycle
		}
		ids[parent] = true
		current = parent
	}

	// Down: children, grandchildren, … breadth-first.
	frontier := []string{facilityID}
	for i := 0; i < maxHierarchyDepth && len(frontier) > 0; i++ {
		var rows []domain.Facility
		if err := tx.Select("id", "parent_id").Find(&rows, "parent_id IN ?", frontier).Error; err != nil {
			return nil, err
		}
		var next []string
		for _, r := range rows {
			if !ids[r.ID] {
				ids[r.ID] = true
				next = append(next, r.ID)
			}
		}
		frontier = next
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
