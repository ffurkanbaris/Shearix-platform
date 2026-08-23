package domain

import (
	"github.com/google/uuid"
	"time"
)

type Role string

const (
	RoleOwner        Role = "OWNER"
	RoleManager      Role = "MANAGER"
	RoleBarber       Role = "BARBER"
	RoleReceptionist Role = "RECEPTIONIST"
)

func (r Role) Valid() bool {
	return r == RoleOwner || r == RoleManager || r == RoleBarber || r == RoleReceptionist
}

type Principal struct {
	IdentityID         uuid.UUID `json:"identity_id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	Phone              string    `json:"phone"`
	MustChangePassword bool      `json:"must_change_password"`
	Role               Role      `json:"role"`
	TenantID           uuid.UUID `json:"tenant_id"`
	SessionID          uuid.UUID `json:"session_id"`
}
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone,omitempty"`
	// Role is deliberately restricted by the application service to ordinary
	// staff roles. OWNER is provisioned only by privileged platform setup.
	Role Role `json:"role"`
}

// Member is a tenant-scoped view of a global identity. It never includes a
// password hash, session, or credential-delivery state.
type Member struct {
	IdentityID uuid.UUID `json:"identity_id"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	Phone      string    `json:"phone"`
	Role       Role      `json:"role"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ChangeMemberRoleInput struct {
	Role Role `json:"role"`
}

type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type ForgotPasswordInput struct {
	Email string `json:"email"`
}

type Registration struct {
	IdentityID          uuid.UUID `json:"identity_id"`
	MembershipCreated   bool      `json:"membership_created"`
	CredentialScheduled bool      `json:"credential_scheduled"`
	Role                Role      `json:"role"`
}
type Session struct {
	Principal Principal
	Token     string
	ExpiresAt time.Time
}
