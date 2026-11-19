package models

import "gorm.io/gorm"

// keptVersions is how many earlier versions of each configuration file are kept.
const keptVersions = 30

// ConfigVersion is the content a configuration file had before a save.
type ConfigVersion struct {
	gorm.Model
	Path    string `gorm:"index"`
	Content string
}

// SaveConfigVersion keeps content as an earlier version of path. Only the
// latest keptVersions versions of each file stay.
func SaveConfigVersion(path, content string) error {
	db := GetDbClient()
	if err := db.Create(&ConfigVersion{Path: path, Content: content}).Error; err != nil {
		return err
	}
	var ids []uint
	if err := db.Model(&ConfigVersion{}).Where("path = ?", path).Order("id desc").Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) <= keptVersions {
		return nil
	}
	return db.Unscoped().Delete(&ConfigVersion{}, ids[keptVersions:]).Error
}

// ConfigVersions returns the earlier versions of path, newest first.
func ConfigVersions(path string) (versions []ConfigVersion) {
	GetDbClient().Where("path = ?", path).Order("id desc").Find(&versions)
	return
}

// FindConfigVersion returns the version with this id.
func FindConfigVersion(id uint) (ConfigVersion, bool) {
	var version ConfigVersion
	err := GetDbClient().First(&version, id).Error
	return version, err == nil
}
