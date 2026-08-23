package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/barber-appointment/notification-service/internal/domain"
	"github.com/barber-appointment/notification-service/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationClaimFencingIntegration(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("NOTIFICATION_TEST_DATABASE_URL"), os.Getenv("NOTIFICATION_TEST_OWNER_DATABASE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("NOTIFICATION_TEST_DATABASE_URL and NOTIFICATION_TEST_OWNER_DATABASE_URL are required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err = owner.Exec(ctx, "SET ROLE notification_db_owner"); err != nil {
		t.Fatal(err)
	}
	tenant, eventID := uuid.New(), uuid.New()
	defer func() { _, _ = owner.Exec(ctx, `DELETE FROM public.email_notifications WHERE tenant_id=$1`, tenant) }()
	repo := repository.New(pool)
	created, err := repo.Plan(ctx, tenant, domain.Envelope{EventID: eventID, AggregateID: uuid.New()}, "appointment_created", "recipient@example.test", "appointment_created", "en", time.Now().Add(-time.Second))
	if err != nil || !created {
		t.Fatalf("plan created=%v err=%v", created, err)
	}
	first, err := repo.Claim(ctx, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim len=%d err=%v", len(first), err)
	}
	if _, err = owner.Exec(ctx, `UPDATE public.email_notifications SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first[0].ID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.Claim(ctx, 1)
	if err != nil || len(second) != 1 || second[0].ClaimToken == first[0].ClaimToken {
		t.Fatalf("reclaim len=%d err=%v", len(second), err)
	}
	if err = repo.Finish(ctx, first[0].ID, first[0].ClaimToken, "stale-message", "", nil); err != nil {
		t.Fatal(err)
	}
	var status string
	var currentClaim uuid.UUID
	var messageID *string
	if err = owner.QueryRow(ctx, `SELECT status,claim_token,provider_message_id FROM public.email_notifications WHERE id=$1`, first[0].ID).Scan(&status, &currentClaim, &messageID); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || currentClaim != second[0].ClaimToken || messageID != nil {
		t.Fatalf("stale worker changed row status=%s claim=%s message=%v", status, currentClaim, messageID)
	}
	if err = repo.Finish(ctx, second[0].ID, second[0].ClaimToken, "final-message", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = owner.QueryRow(ctx, `SELECT status,provider_message_id FROM public.email_notifications WHERE id=$1`, first[0].ID).Scan(&status, &messageID); err != nil || status != "sent" || messageID == nil || *messageID != "final-message" {
		t.Fatalf("claim owner did not finish status=%s message=%v err=%v", status, messageID, err)
	}
}
