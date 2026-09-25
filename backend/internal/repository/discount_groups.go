package repository

import (
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) DiscountGroup(id string) (*model.DiscountGroup, error) {
	var group model.DiscountGroup
	if err := r.db.First(&group, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *Repository) DiscountGroupsByIDs(ids []string) ([]model.DiscountGroup, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var groups []model.DiscountGroup
	if err := r.db.Where("id IN ?", ids).Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func (r *Repository) AdminDiscountGroups(keyword string, status string, limit int, offset int) ([]model.DiscountGroup, int64, error) {
	var items []model.DiscountGroup
	var total int64
	query := r.db.Model(&model.DiscountGroup{})
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	switch strings.TrimSpace(status) {
	case "enabled":
		query = query.Where("enabled = ?", true)
	case "disabled":
		query = query.Where("enabled = ?", false)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	memberSubquery := r.db.Model(&model.User{}).Select("COUNT(*)").Where("users.discount_group_id = discount_groups.id")
	err := query.Select("discount_groups.*, (?) AS member_count", memberSubquery).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error
	return items, total, err
}

func (r *Repository) AdminDiscountGroupReferences() ([]model.DiscountGroup, error) {
	var groups []model.DiscountGroup
	err := r.db.Model(&model.DiscountGroup{}).Order("name ASC").Find(&groups).Error
	return groups, err
}

func (r *Repository) CreateDiscountGroup(group *model.DiscountGroup) error {
	return r.db.Create(group).Error
}

func (r *Repository) SaveDiscountGroup(group *model.DiscountGroup) error {
	return r.db.Save(group).Error
}

func (r *Repository) DeleteDiscountGroup(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("discount_group_id = ?", id).Update("discount_group_id", nil).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.DiscountGroup{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (r *Repository) DiscountGroupNameExists(name string, excludeID string) (bool, error) {
	query := r.db.Model(&model.DiscountGroup{}).Where("name = ?", strings.TrimSpace(name))
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
