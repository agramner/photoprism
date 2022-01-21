package photoprism

import (
	"fmt"

	"github.com/photoprism/photoprism/internal/entity"
	"github.com/photoprism/photoprism/internal/face"
	"github.com/photoprism/photoprism/internal/query"
)

// FacesOptimizeResult represents the outcome of Faces.Optimize().
type FacesOptimizeResult struct {
	Merged int
}

// Optimize optimizes the face lookup table.
func (w *Faces) Optimize() (result FacesOptimizeResult, err error) {
	if w.Disabled() {
		return result, fmt.Errorf("facial recognition is disabled")
	}

	// Fetch manually added faces from the database.
	allFaces, err := query.ManuallyAddedFaces(false)
	if err != nil {
		return result, err
	}

	// Need at least 2 faces to optimize.
	if len(allFaces) < 2 {
		return result, nil
	}

	// Group faces by subject UID
	groups := map[string]entity.Faces{}
	for _, face := range allFaces {
		if face.SubjUID == "" {
			// TODO: Unncessary?
			continue
		}

		if _, exists := groups[face.SubjUID]; !exists {
			groups[face.SubjUID] = entity.Faces{}
		}

		groups[face.SubjUID] = append(groups[face.SubjUID], face)
	}

	// For each subject group, try to find pairs of matching faces and merge them if found
	for subjUID, faces := range groups {
		paired := map[string]struct{}{}
		for i := range faces {
			for j := range faces {
				if i == j {
					// Skip matching faces with them self
					continue
				}

				if _, exist := paired[faces[i].ID]; exist {
					// Skip this face, already been paired
					continue
				}

				if _, exist := paired[faces[j].ID]; exist {
					// Skip this face, already been paired
					continue
				}

				faceA := faces[i]
				faceB := faces[j]
				match, dist := faceA.Match(face.Embeddings{faceB.Embedding()})
				if !match {
					// No match, continue
					continue
				}

				log.Debugf("faces: can merge %s with %s, subject %s, dist %f", faceA.ID, faceB.ID, subjUID, dist)

				// Mark the faces as already paired
				paired[faceA.ID] = struct{}{}
				paired[faceB.ID] = struct{}{}

				// Merge the paired faces
				if _, err := query.MergeFaces(entity.Faces{faceA, faceB}); err != nil {
					log.Errorf("%s (merge)", err)
					continue
				}

				result.Merged += 2
			}
		}
	}

	return result, nil
}
