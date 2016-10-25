package datastore

import (
	"sync"

	"gitlab.com/conspico/esh/vcs"

	"github.com/jinzhu/gorm"
)

type vcsDatastore struct {
	mtx sync.RWMutex
	db  *gorm.DB
}

func (r *vcsDatastore) Save(v *vcs.VCS) error {

	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.db.NewRecord(v)
	err := r.db.Create(&v).Error

	return err
}

func (r *vcsDatastore) GetVCS(teamID string) ([]vcs.VCS, error) {

	r.mtx.Lock()
	defer r.mtx.Unlock()

	var result []vcs.VCS
	err := r.db.Raw(`SELECT id, 
							name, 
							type, 
							avatar_url,
							updated_dt
				     FROM VCS WHERE TEAM_ID = ? LIMIT 1`, teamID).Scan(&result).Error
	return result, err
}

func (r *vcsDatastore) GetByID(id string) (vcs.VCS, error) {

	r.mtx.Lock()
	defer r.mtx.Unlock()

	var result vcs.VCS
	err := r.db.Raw(`SELECT id, 
							name, 
							type, 
							avatar_url,
							access_token,
							refresh_token,
							token_expiry,
							token_type
				     FROM VCS WHERE ID = ? LIMIT 1`, id).Scan(&result).Error
	return result, err
}

func (r *vcsDatastore) Update(old *vcs.VCS, updated vcs.VCS) error {

	r.mtx.Lock()
	defer r.mtx.Unlock()

	err := r.db.Model(old).Updates(updated).Error
	return err
}

// NewVCS ..
func NewVCS(db *gorm.DB) vcs.Datastore {
	return &vcsDatastore{db: db}
}