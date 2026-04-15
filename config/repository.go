package config

// Repository defines the configuration persistence boundary used by higher layers.
type Repository interface {
	Load() (Config, error)
	Save(conf *Config) error
	ResetPassword(newPassword string) error
}

// CachedFileRepository persists configuration through the existing cached YAML file implementation.
type CachedFileRepository struct{}

func NewRepository() Repository {
	return &CachedFileRepository{}
}

func (r *CachedFileRepository) Load() (Config, error) {
	return GetConfigCached()
}

func (r *CachedFileRepository) Save(conf *Config) error {
	return conf.SaveConfig()
}

func (r *CachedFileRepository) ResetPassword(newPassword string) error {
	conf, err := r.Load()
	if err != nil {
		return err
	}
	return conf.ResetPassword(newPassword)
}
