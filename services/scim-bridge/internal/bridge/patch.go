package bridge

import (
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// snapshotFromPayload captures the subset of fields we track for diffing.
func snapshotFromPayload(payload mas.UserResource, profileID string) state.Snapshot {
	s := state.Snapshot{
		UserName:  payload.UserName,
		ProfileID: profileID,
		Active:    payload.Active,
		HasActive: true,
	}
	if payload.Name != nil {
		s.GivenName = payload.Name.GivenName
		s.FamilyName = payload.Name.FamilyName
	}
	if len(payload.Emails) > 0 {
		s.Email = payload.Emails[0].Value
		s.HasEmail = true
	}
	return s
}

// diffToPatch computes SCIM Patch operations between the previous snapshot and the new payload.
func diffToPatch(prev state.Snapshot, payload mas.UserResource, profileID string) (state.Snapshot, []mas.PatchOperation) {
	next := snapshotFromPayload(payload, profileID)
	ops := make([]mas.PatchOperation, 0, 5)

	if prev.UserName != "" && prev.UserName != next.UserName {
		ops = append(ops, mas.PatchOperation{Op: "replace", Path: "userName", Value: next.UserName})
	} else if prev.UserName == "" && next.UserName != "" {
		ops = append(ops, mas.PatchOperation{Op: "add", Path: "userName", Value: next.UserName})
	}

	if prev.GivenName != next.GivenName {
		if next.GivenName == "" && prev.GivenName != "" {
			ops = append(ops, mas.PatchOperation{Op: "remove", Path: "name.givenName"})
		} else if next.GivenName != "" {
			ops = append(ops, mas.PatchOperation{Op: "replace", Path: "name.givenName", Value: next.GivenName})
		}
	}
	if prev.FamilyName != next.FamilyName {
		if next.FamilyName == "" && prev.FamilyName != "" {
			ops = append(ops, mas.PatchOperation{Op: "remove", Path: "name.familyName"})
		} else if next.FamilyName != "" {
			ops = append(ops, mas.PatchOperation{Op: "replace", Path: "name.familyName", Value: next.FamilyName})
		}
	}

	if prev.HasEmail != next.HasEmail || prev.Email != next.Email {
		if next.HasEmail {
			ops = append(ops, mas.PatchOperation{
				Op:   "replace",
				Path: "emails",
				Value: []mas.EmailResource{{
					Value:   next.Email,
					Primary: true,
				}},
			})
		} else if prev.HasEmail {
			ops = append(ops, mas.PatchOperation{Op: "remove", Path: "emails"})
		}
	}

	if prev.HasActive && prev.Active != next.Active {
		ops = append(ops, mas.PatchOperation{Op: "replace", Path: "active", Value: next.Active})
	} else if !prev.HasActive && next.HasActive {
		ops = append(ops, mas.PatchOperation{Op: "add", Path: "active", Value: next.Active})
	}

	return next, ops
}
