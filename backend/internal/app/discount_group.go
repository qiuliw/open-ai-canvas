package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type DiscountGroupPage struct {
	Groups []model.DiscountGroup `json:"groups"`
	Total  int64                 `json:"total"`
	Page   int                   `json:"page"`
	Limit  int                   `json:"pageSize"`
}

type DiscountGroupRequest struct {
	Name                   string           `json:"name"`
	Description            string           `json:"description"`
	DefaultMultiplierBPS   int64            `json:"defaultMultiplierBasisPoints"`
	ModelMultiplierBPS     map[string]int64 `json:"modelMultiplierBasisPoints"`
	Enabled                *bool            `json:"enabled"`
}

type AdminDiscountGroupReference struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func encodeDiscountGroupModelMultipliers(multipliers map[string]int64) (string, error) {
	if multipliers == nil {
		multipliers = map[string]int64{}
	}
	encoded, err := json.Marshal(multipliers)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeDiscountGroupModelMultipliers(raw string) map[string]int64 {
	result := map[string]int64{}
	if strings.TrimSpace(raw) == "" {
		return result
	}
	_ = json.Unmarshal([]byte(raw), &result)
	if result == nil {
		return map[string]int64{}
	}
	return result
}

func hydrateDiscountGroup(group *model.DiscountGroup) {
	if group == nil {
		return
	}
	group.ModelMultiplierBPS = decodeDiscountGroupModelMultipliers(group.ModelMultiplierBasisPointsJSON)
}

func hydrateDiscountGroups(groups []model.DiscountGroup) []model.DiscountGroup {
	for index := range groups {
		hydrateDiscountGroup(&groups[index])
	}
	return groups
}

func validateDiscountGroupRequest(req DiscountGroupRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return BadAuthRequest("请填写折扣分组名称")
	}
	if len([]rune(name)) > 80 {
		return BadAuthRequest("折扣分组名称不能超过 80 字")
	}
	if len([]rune(strings.TrimSpace(req.Description))) > 500 {
		return BadAuthRequest("折扣分组说明不能超过 500 字")
	}
	if req.DefaultMultiplierBPS <= 0 || req.DefaultMultiplierBPS > 1_000_000 {
		return BadAuthRequest("默认模型倍率必须在 0.0001-100 之间")
	}
	for modelKey, multiplier := range req.ModelMultiplierBPS {
		if strings.TrimSpace(modelKey) == "" || multiplier <= 0 || multiplier > 1_000_000 {
			return BadAuthRequest("模型倍率配置无效")
		}
	}
	return nil
}

func (s *Service) AdminDiscountGroupPage(actor *model.User, query AdminListQuery) (*DiscountGroupPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	items, total, err := s.repo.AdminDiscountGroups(query.Keyword, query.Status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &DiscountGroupPage{Groups: hydrateDiscountGroups(items), Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminCreateDiscountGroup(actor *model.User, req DiscountGroupRequest) (*model.DiscountGroup, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if req.ModelMultiplierBPS == nil {
		req.ModelMultiplierBPS = map[string]int64{}
	}
	if err := validateDiscountGroupRequest(req); err != nil {
		return nil, err
	}
	name := truncateRunes(strings.TrimSpace(req.Name), 80)
	exists, err := s.repo.DiscountGroupNameExists(name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, BadAuthRequest("折扣分组名称已存在")
	}
	encoded, err := encodeDiscountGroupModelMultipliers(req.ModelMultiplierBPS)
	if err != nil {
		return nil, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now()
	group := &model.DiscountGroup{
		ID:                             newID(),
		Name:                           name,
		Description:                    truncateRunes(strings.TrimSpace(req.Description), 500),
		DefaultMultiplierBPS:           req.DefaultMultiplierBPS,
		ModelMultiplierBasisPointsJSON: encoded,
		ModelMultiplierBPS:             req.ModelMultiplierBPS,
		Enabled:                        enabled,
		CreatedAt:                      now,
		UpdatedAt:                      now,
	}
	if err := s.repo.CreateDiscountGroup(group); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "discount_group.create", "discount_group", group.ID, "创建折扣分组", map[string]any{
		"name": group.Name, "defaultMultiplierBasisPoints": group.DefaultMultiplierBPS, "enabled": group.Enabled,
	}); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *Service) AdminUpdateDiscountGroup(actor *model.User, id string, req DiscountGroupRequest) (*model.DiscountGroup, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	group, err := s.repo.DiscountGroup(strings.TrimSpace(id))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("折扣分组不存在")
	}
	if err != nil {
		return nil, err
	}
	if req.ModelMultiplierBPS == nil {
		req.ModelMultiplierBPS = map[string]int64{}
	}
	if err := validateDiscountGroupRequest(req); err != nil {
		return nil, err
	}
	name := truncateRunes(strings.TrimSpace(req.Name), 80)
	exists, err := s.repo.DiscountGroupNameExists(name, group.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, BadAuthRequest("折扣分组名称已存在")
	}
	encoded, err := encodeDiscountGroupModelMultipliers(req.ModelMultiplierBPS)
	if err != nil {
		return nil, err
	}
	group.Name = name
	group.Description = truncateRunes(strings.TrimSpace(req.Description), 500)
	group.DefaultMultiplierBPS = req.DefaultMultiplierBPS
	group.ModelMultiplierBasisPointsJSON = encoded
	group.ModelMultiplierBPS = req.ModelMultiplierBPS
	if req.Enabled != nil {
		group.Enabled = *req.Enabled
	}
	group.UpdatedAt = time.Now()
	if err := s.repo.SaveDiscountGroup(group); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "discount_group.update", "discount_group", group.ID, "更新折扣分组", map[string]any{
		"name": group.Name, "defaultMultiplierBasisPoints": group.DefaultMultiplierBPS, "enabled": group.Enabled,
	}); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *Service) AdminDeleteDiscountGroup(actor *model.User, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return BadAuthRequest("折扣分组 ID 无效")
	}
	if _, err := s.repo.DiscountGroup(id); errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("折扣分组不存在")
	} else if err != nil {
		return err
	}
	if err := s.repo.DeleteDiscountGroup(id); err != nil {
		return err
	}
	return s.appendAdminAudit(actor, "discount_group.delete", "discount_group", id, "删除用户倍率分组并清除用户分配", nil)
}

// resolveBillingMultiplierBPS 解析用户最终计费倍率。
// 先取全局策略（模型覆盖 → 默认），再与启用中的用户倍率分组相乘叠加。
func (s *Service) resolveBillingMultiplierBPS(userID string, modelKey string) (int64, error) {
	policy, err := s.creditPolicy()
	if err != nil {
		return 0, err
	}
	multiplierBPS := policy.DefaultMultiplierBPS
	if configured := policy.ModelMultiplierBPS[modelKey]; configured > 0 {
		multiplierBPS = configured
	}
	groupBPS, ok, err := s.userDiscountGroupMultiplierBPS(userID, modelKey)
	if err != nil {
		return 0, err
	}
	if !ok {
		return multiplierBPS, nil
	}
	return multiplyBasisPoints(multiplierBPS, groupBPS)
}

func multiplyBasisPoints(left int64, right int64) (int64, error) {
	if left <= 0 || right <= 0 {
		return 0, errors.New("积分倍率无效")
	}
	if left > (1<<63-1)/right {
		return 0, errors.New("积分倍率溢出")
	}
	product := left * right
	if product < 10_000 {
		return 1, nil
	}
	return product / 10_000, nil
}

// userDiscountGroupMultiplierBPS 仅读取启用中的用户倍率分组；未分配或已停用时返回 ok=false。
func (s *Service) userDiscountGroupMultiplierBPS(userID string, modelKey string) (int64, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0, false, nil
	}
	user, err := s.repo.User(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if user.DiscountGroupID == nil || strings.TrimSpace(*user.DiscountGroupID) == "" {
		return 0, false, nil
	}
	group, err := s.repo.DiscountGroup(strings.TrimSpace(*user.DiscountGroupID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	hydrateDiscountGroup(group)
	if !group.Enabled {
		return 0, false, nil
	}
	multiplierBPS := group.DefaultMultiplierBPS
	if configured := group.ModelMultiplierBPS[modelKey]; configured > 0 {
		multiplierBPS = configured
	}
	return multiplierBPS, true, nil
}
