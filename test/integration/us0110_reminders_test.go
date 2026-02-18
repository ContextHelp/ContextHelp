package integration

import (
	"context"
	"encoding/json"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestUS0110_ListReminders(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	reminder := &storage.SystemReminder{
		ID:        uuid.New().String(),
		Type:      "update",
		Title:     "Test reminder",
		Message:   "Test update available",
		Source:    "test",
		ActionURL: "http://example.com",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Reminders().Create(ctx, reminder))

	resp, err := gohttp.Get(env.URL + "/api/v1/system/reminders")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Reminders []storage.SystemReminder `json:"reminders"`
		Total     int                      `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.GreaterOrEqual(t, len(body.Reminders), 1)
}

func TestUS0110_DismissReminder(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	reminder := &storage.SystemReminder{
		ID:        uuid.New().String(),
		Type:      "update",
		Title:     "Dismiss test",
		Message:   "Will be dismissed",
		Source:    "test",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Reminders().Create(ctx, reminder))

	resp, err := gohttp.Post(env.URL+"/api/v1/system/reminders/"+reminder.ID+"/dismiss", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)
}

func TestUS0110_ActiveOnlyFilter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()

	activeReminder := &storage.SystemReminder{
		ID: uuid.New().String(), Type: "update", Title: "active-reminder",
		Message: "Active", Source: "test", Dismissed: false,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Reminders().Create(ctx, activeReminder))

	dismissedReminder := &storage.SystemReminder{
		ID: uuid.New().String(), Type: "update", Title: "dismissed-reminder",
		Message: "Dismissed", Source: "test", Dismissed: true,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Reminders().Create(ctx, dismissedReminder))

	t.Run("active_only excludes dismissed", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/system/reminders?active_only=true")
		require.NoError(t, err)
		defer resp.Body.Close()

		var body struct {
			Reminders []storage.SystemReminder `json:"reminders"`
			Total     int                      `json:"total"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, 1, body.Total)
		require.Len(t, body.Reminders, 1)
		assert.Equal(t, "active-reminder", body.Reminders[0].Title)
	})

	t.Run("no filter returns all", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/system/reminders")
		require.NoError(t, err)
		defer resp.Body.Close()

		var body struct {
			Reminders []storage.SystemReminder `json:"reminders"`
			Total     int                      `json:"total"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.GreaterOrEqual(t, body.Total, 2)
	})
}

func TestUS0110_DismissThenVerify(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	reminderID := uuid.New().String()

	reminder := &storage.SystemReminder{
		ID: reminderID, Type: "info", Title: "verify-dismiss",
		Message: "Reminder to be dismissed", Source: "test",
		Dismissed: false, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Reminders().Create(ctx, reminder))

	resp, err := gohttp.Get(env.URL + "/api/v1/system/reminders?active_only=true")
	require.NoError(t, err)
	defer resp.Body.Close()

	var beforeBody struct {
		Reminders []storage.SystemReminder `json:"reminders"`
		Total     int                      `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&beforeBody))

	found := false
	for _, r := range beforeBody.Reminders {
		if r.Title == "verify-dismiss" {
			found = true
			break
		}
	}
	assert.True(t, found, "reminder should appear before dismissal")

	respDismiss, err := gohttp.Post(env.URL+"/api/v1/system/reminders/"+reminderID+"/dismiss", "application/json", nil)
	require.NoError(t, err)
	defer respDismiss.Body.Close()
	require.Equal(t, gohttp.StatusOK, respDismiss.StatusCode)

	respAfter, err := gohttp.Get(env.URL + "/api/v1/system/reminders?active_only=true")
	require.NoError(t, err)
	defer respAfter.Body.Close()

	var afterBody struct {
		Reminders []storage.SystemReminder `json:"reminders"`
	}
	require.NoError(t, json.NewDecoder(respAfter.Body).Decode(&afterBody))

	for _, r := range afterBody.Reminders {
		assert.NotEqual(t, "verify-dismiss", r.Title, "dismissed reminder should not appear in active list")
	}
}

func TestUS0110_MultipleTypes(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()

	types := []struct {
		typ   string
		title string
	}{
		{"update", "rem-update"},
		{"alert", "rem-alert"},
		{"info", "rem-info"},
	}

	for _, tt := range types {
		reminder := &storage.SystemReminder{
			ID: uuid.New().String(), Type: tt.typ, Title: tt.title,
			Message: "Reminder of type " + tt.typ, Source: "test",
			CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, env.svc.Store.Reminders().Create(ctx, reminder))
	}

	resp, err := gohttp.Get(env.URL + "/api/v1/system/reminders")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Reminders []storage.SystemReminder `json:"reminders"`
		Total     int                      `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 3)

	titles := make([]string, len(body.Reminders))
	for i, r := range body.Reminders {
		titles[i] = r.Title
	}

	assert.Contains(t, titles, "rem-update")
	assert.Contains(t, titles, "rem-alert")
	assert.Contains(t, titles, "rem-info")
}

func TestUS0110_DismissNonExistentIsIdempotent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Post(env.URL+"/api/v1/system/reminders/nonexistent-id-xyz/dismiss", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Dismiss is idempotent — store silently accepts missing IDs.
	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "dismiss should be idempotent for non-existent IDs")
}
