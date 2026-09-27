package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVideoTaskTimeoutHasFiveMinuteSafetyFloor(t *testing.T) {
	policy := defaultRuntimePolicy().Task
	policy.VideoTimeoutMinutes = 1
	policy.ImageTimeoutMinutes = 1
	if got := taskExecutionTimeoutWithPolicy("canvas_video", policy); got != 5*time.Minute {
		t.Fatalf("video timeout = %s, want 5m", got)
	}
	if got := taskExecutionTimeoutWithPolicy("canvas_image", policy); got != time.Minute {
		t.Fatalf("image timeout = %s, want 1m", got)
	}
}

func TestDefaultVideoTaskTimeoutIsOneHour(t *testing.T) {
	policy := defaultRuntimePolicy().Task
	if policy.VideoTimeoutMinutes != 60 {
		t.Fatalf("default video timeout = %d minutes, want 60", policy.VideoTimeoutMinutes)
	}
	if got := taskExecutionTimeoutWithPolicy("canvas_video", policy); got != time.Hour {
		t.Fatalf("video execution timeout = %s, want 1h", got)
	}
}

func TestProviderPollingDeadlineDefaultsToOneHour(t *testing.T) {
	startedAt := time.Now()
	deadline := providerPollingDeadline(context.Background())
	remaining := deadline.Sub(startedAt)
	if remaining < time.Hour || remaining > time.Hour+time.Second {
		t.Fatalf("provider polling deadline = %s, want approximately 1h", remaining)
	}
}

func TestResumableVideoDeadlineKeepsRunningWhenProviderTaskExists(t *testing.T) {
	svc := &Service{}
	startedAt := time.Now()
	base := model.Task{ID: "task-1", Type: "canvas_video", ProviderRequestID: "provider-task-1", StartedAt: &startedAt}
	if !svc.shouldDeferVideoProviderTask(base, context.DeadlineExceeded) {
		t.Fatal("video deadline with ProviderRequestID should remain running")
	}
	pendingErr := providerFailure{StatusCode: 400, Body: `{"code":"task_not_exist"}`, Pending: true, TaskID: base.ProviderRequestID}
	if !svc.shouldDeferVideoProviderTask(base, pendingErr) {
		t.Fatal("bounded provider sync delay should remain running")
	}
	expiredStartedAt := startedAt.Add(-videoProviderPendingSyncMaxAge - time.Second)
	expiredTask := model.Task{ID: base.ID, Type: base.Type, ProviderRequestID: base.ProviderRequestID, StartedAt: &expiredStartedAt}
	if svc.shouldDeferVideoProviderTask(expiredTask, pendingErr) {
		t.Fatal("provider sync delay must stop after the bounded synchronization window")
	}
	if !svc.shouldDeferVideoProviderTask(expiredTask, context.DeadlineExceeded) {
		t.Fatal("a normal long-running provider task must remain resumable after the synchronization window")
	}
	if svc.shouldDeferVideoProviderTask(model.Task{ID: base.ID, Type: base.Type, ProviderRequestID: base.ProviderRequestID}, pendingErr) {
		t.Fatal("provider sync delay without a persisted start time must fail closed")
	}
	if svc.shouldDeferVideoProviderTask(model.Task{ID: "task-2", Type: "canvas_video"}, context.DeadlineExceeded) {
		t.Fatal("missing provider task id must not be deferred")
	}
	if svc.shouldDeferVideoProviderTask(model.Task{ID: "task-2", Type: "canvas_video"}, pendingErr) {
		t.Fatal("provider sync delay without persisted provider task id must not be deferred")
	}
	if svc.shouldDeferVideoProviderTask(base, context.Canceled) {
		t.Fatal("explicit cancellation must not be deferred")
	}
	if svc.shouldDeferVideoProviderTask(base, providerFailure{StatusCode: 400, Body: `{"code":"task_not_exist"}`}) {
		t.Fatal("untyped provider error must not be deferred")
	}
	// Protocol ID is irrelevant: any video channel that already has an upstream ID resumes poll-only.
	if !svc.shouldDeferVideoProviderTask(base, context.DeadlineExceeded) {
		t.Fatal("defer must not depend on InterfaceType")
	}
}

func TestRuntimePolicyDefaultsAndSelfUseModeValidate(t *testing.T) {
	if err := validateRuntimePolicy(defaultRuntimePolicy()); err != nil {
		t.Fatalf("default runtime policy error = %v", err)
	}
	selfUse := selfUseRuntimePolicy()
	if err := validateRuntimePolicy(selfUse); err != nil {
		t.Fatalf("self-use runtime policy error = %v", err)
	}
	if selfUse.Task.WorkerConcurrency != 999 || selfUse.Resource.ResourceUploadMB != 999 {
		t.Fatalf("self-use maxima = worker %d, upload %d", selfUse.Task.WorkerConcurrency, selfUse.Resource.ResourceUploadMB)
	}
}

func TestRuntimePolicyRejectsSingleFileAboveAccountCapacity(t *testing.T) {
	policy := defaultRuntimePolicy()
	policy.Resource.StoredFileGB = 1
	policy.Resource.ResourceUploadMB = 999
	if err := validateRuntimePolicy(policy); err != nil {
		t.Fatalf("999MB should fit in 1GB: %v", err)
	}
	policy.Resource.StoredFileGB = 0
	if err := validateRuntimePolicy(policy); err == nil {
		t.Fatal("zero account capacity should be rejected")
	}
}

func TestRuntimePolicySaveAndResetTakeEffectImmediately(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	actor := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	policy := defaultRuntimePolicy()
	policy.Task.ActiveTaskLimit = 17
	if _, err := svc.UpdateRuntimePolicySetting(actor, policy); err != nil {
		t.Fatal(err)
	}
	effective, err := svc.RuntimePolicy()
	if err != nil || effective.Task.ActiveTaskLimit != 17 {
		t.Fatalf("effective active task limit = %d, error = %v", effective.Task.ActiveTaskLimit, err)
	}
	if _, err := svc.ResetRuntimePolicySetting(actor); err != nil {
		t.Fatal(err)
	}
	effective, err = svc.RuntimePolicy()
	if err != nil || effective.Task.ActiveTaskLimit != 5 {
		t.Fatalf("reset active task limit = %d, error = %v", effective.Task.ActiveTaskLimit, err)
	}
}

func TestRuntimePolicyBackfillsRecycleBinRetentionForLegacyJSON(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	legacy := defaultRuntimePolicy()
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	resource := value["resource"].(map[string]any)
	delete(resource, "recycleBinRetentionDays")
	encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: runtimePolicySettingKey, ValueJSON: string(encoded), UpdatedBy: "admin"}).Error; err != nil {
		t.Fatal(err)
	}

	policy, err := New(repository.New(db), t.TempDir()).RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	if policy.Resource.RecycleBinRetentionDays != 30 {
		t.Fatalf("legacy recycle bin retention = %d, want 30", policy.Resource.RecycleBinRetentionDays)
	}
}
