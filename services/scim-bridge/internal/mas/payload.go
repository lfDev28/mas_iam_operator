package mas

import "github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"

// UserResource represents the subset of SCIM fields we currently populate.
type UserResource struct {
	Schemas     []string        `json:"schemas"`
	UserName    string          `json:"userName"`
	ExternalID  string          `json:"externalId,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	Active      bool            `json:"active"`
	Emails      []EmailResource `json:"emails,omitempty"`
	Name        *NameResource   `json:"name,omitempty"`
}

// EmailResource captures SCIM email entries.
type EmailResource struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// NameResource maps Keycloak first/last names.
type NameResource struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

// BuildUserPayload constructs a SCIM user body from a Keycloak user.
func BuildUserPayload(u keycloak.User) UserResource {
	payload := UserResource{
		Schemas:    []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		UserName:   u.Username,
		ExternalID: u.ID,
		Active:     u.Enabled,
	}
	if u.FirstName != "" || u.LastName != "" {
		payload.Name = &NameResource{GivenName: u.FirstName, FamilyName: u.LastName}
		payload.DisplayName = buildDisplayName(u)
	}
	if u.Email != "" {
		payload.Emails = []EmailResource{{Value: u.Email, Primary: true}}
	}
	return payload
}

func buildDisplayName(u keycloak.User) string {
	switch {
	case u.FirstName == "" && u.LastName == "":
		return u.Username
	case u.FirstName == "":
		return u.LastName
	case u.LastName == "":
		return u.FirstName
	default:
		return u.FirstName + " " + u.LastName
	}
}
