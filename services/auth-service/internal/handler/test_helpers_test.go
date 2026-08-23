package handler

import (
	"errors"
	"github.com/barber-appointment/auth-service/internal/repository"
	"github.com/barber-appointment/auth-service/internal/service"
)

type nilVerifier struct{}

func (nilVerifier) Verify(string) error { return errors.New("not trusted") }
func serviceZero() service.Service      { return service.New(repository.Repository{}, 0) }
