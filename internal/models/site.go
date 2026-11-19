package models

import "gorm.io/gorm"

// DisabledSite is a site the admin disabled: nginx no longer loads it, and
// its configuration is kept so it can be enabled again.
type DisabledSite struct {
	gorm.Model
	// Path is where nginx loaded the site from, and where it goes back.
	Path string `gorm:"uniqueIndex"`
	// Link is the target of Path when it was a symbolic link (Debian's
	// sites-enabled): disabling removes the link and keeps its target, as
	// a2dissite does.
	Link string
	// Stored is where the configuration file waits while the site is disabled
	// (empty for a link).
	Stored string
}

// DisabledSites returns the disabled sites, oldest first.
func DisabledSites() (sites []DisabledSite) {
	GetDbClient().Order("id").Find(&sites)
	return
}

// FindDisabledSite returns the disabled site with this id.
func FindDisabledSite(id uint) (DisabledSite, bool) {
	var site DisabledSite
	err := GetDbClient().First(&site, id).Error
	return site, err == nil
}

// Forget removes the record of a disabled site.
func (s *DisabledSite) Forget() error {
	return GetDbClient().Unscoped().Delete(s).Error
}
