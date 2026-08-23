package service

import "github.com/google/uuid"

func parseTenantID(value string) (uuid.UUID, error) { return uuid.Parse(value) }
