package domain

import "github.com/google/uuid"

type Branch struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address string    `json:"address"`
	Active  bool      `json:"active"`
}
type Barber struct {
	ID          uuid.UUID  `json:"id"`
	DisplayName string     `json:"display_name"`
	Bio         string     `json:"bio"`
	Active      bool       `json:"active"`
	IdentityID  *uuid.UUID `json:"identity_id,omitempty"`
	// BranchIDs are an admin-only management view. They keep the business
	// entity separate from its identity link while allowing clients to edit the
	// existing tenant-scoped branch assignments without inference.
	BranchIDs []uuid.UUID `json:"branch_ids,omitempty"`
}
type BranchInput struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Active  *bool  `json:"active,omitempty"`
}
type BarberInput struct {
	DisplayName string      `json:"display_name"`
	Bio         string      `json:"bio"`
	Active      *bool       `json:"active,omitempty"`
	BranchIDs   []uuid.UUID `json:"branch_ids"`
}

type LinkIdentityInput struct {
	IdentityID uuid.UUID `json:"identity_id"`
}
