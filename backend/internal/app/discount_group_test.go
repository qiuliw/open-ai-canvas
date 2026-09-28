package app

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolveBillingMultiplierUsesDiscountGroup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:discount-group-billing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.DiscountGroup{}, &model.SystemSetting{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	policy := CreditPolicy{SignupBonusMicrocredits: 0, CheckinBonusMicrocredits: 0, DefaultMultiplierBPS: 10_000, ModelMultiplierBPS: map[string]int64{"image-model": 12_000}}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: creditPolicySettingKey, ValueJSON: string(encoded)}).Error; err != nil {
		t.Fatal(err)
	}
	groupMultipliers, err := json.Marshal(map[string]int64{"image-model": 5_000})
	if err != nil {
		t.Fatal(err)
	}
	group := model.DiscountGroup{
		ID: "group-vip", Name: "VIP", DefaultMultiplierBPS: 8_000,
		ModelMultiplierBasisPointsJSON: string(groupMultipliers), Enabled: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	groupID := group.ID
	user := model.User{ID: "user-1", Username: "vip", DisplayName: "VIP", Role: model.UserRoleUser, Status: model.UserStatusActive, DiscountGroupID: &groupID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	channelModel := model.ChannelModel{
		ID: "cm-1", ChannelID: "ch-1", ModelKey: "image-model", Capability: "image",
		Protocol: model.ChannelInterfaceOpenAIImage, Enabled: true, PriceConfigured: true,
		PriceTiers: []model.ChannelModelPriceTier{{
			ID: "tier-1", ChannelModelID: "cm-1", SelectorKey: `{}`, SelectorJSON: `{}`,
			BillingMode: "fixed_request", UnitPriceMicrocredits: 1_000_000, Enabled: true, PriceConfigured: true,
		}},
	}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&channelModel.PriceTiers).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}

	multiplier, err := svc.resolveBillingMultiplierBPS(user.ID, "image-model")
	if err != nil || multiplier != 6_000 {
		t.Fatalf("stacked group model override = %d, err=%v", multiplier, err)
	}
	multiplier, err = svc.resolveBillingMultiplierBPS(user.ID, "other-model")
	if err != nil || multiplier != 8_000 {
		t.Fatalf("stacked group default = %d, err=%v", multiplier, err)
	}
	multiplier, err = svc.resolveBillingMultiplierBPS("", "image-model")
	if err != nil || multiplier != 12_000 {
		t.Fatalf("global model without user = %d, err=%v", multiplier, err)
	}

	order, err := svc.newBillingOrderWithPriceTier(user.ID, "task-1", "idem-1", channelModel.ChannelID, channelModel.ModelKey, "image", "test", 1, tokenBillingEstimate{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if order.MultiplierBasisPoints != 6_000 || order.AmountMicrocredits != 600_000 {
		t.Fatalf("order=%#v", order)
	}

	disabled := false
	if _, err := svc.AdminUpdateDiscountGroup(&model.User{ID: "admin", Role: model.UserRoleAdmin}, group.ID, DiscountGroupRequest{
		Name: group.Name, DefaultMultiplierBPS: 8_000, ModelMultiplierBPS: map[string]int64{"image-model": 5_000}, Enabled: &disabled,
	}); err != nil {
		t.Fatal(err)
	}
	multiplier, err = svc.resolveBillingMultiplierBPS(user.ID, "image-model")
	if err != nil || multiplier != 12_000 {
		t.Fatalf("disabled group should fall back to global = %d, err=%v", multiplier, err)
	}
}
