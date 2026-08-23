package tenantctx

import (
	"errors"
	"github.com/google/uuid"
)

const (
	TenantIDHeader  = "X-Tenant-ID"
	AppTypeHeader   = "X-App-Type"
	RequestIDHeader = "X-Request-ID"
)

type Context struct {
	TenantID  uuid.UUID
	AppType   string
	RequestID string
}

func New(tenantID, appType, requestID string) (Context, error) {
	id, err := uuid.Parse(tenantID)
	if err != nil {
		return Context{}, errors.New("invalid tenant id")
	}
	if appType != "booking" && appType != "admin" {
		return Context{}, errors.New("invalid app type")
	}
	return Context{TenantID: id, AppType: appType, RequestID: requestID}, nil
}
