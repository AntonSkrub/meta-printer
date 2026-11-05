// Package config loads Meta-Printer runtime configuration.
//
// Configuration is read from the first JSON file found in the following
// locations (in order):
//
// Missing or unreadable files are silently skipped. Fields absent from the
// file retain their computed default values.
package config

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/valentin-kaiser/go-core/apperror"
	"github.com/valentin-kaiser/go-core/config"
	"github.com/valentin-kaiser/go-core/flag"
)

// DefaultDatabaseDir is shared by metad (running as the desktop user) and
// metafilter (running as lp under CUPS, without a usable home directory).
const DefaultDatabaseDir = "/var/lib/meta-printer"

// Config holds all configurable values for Meta-Printer.
type Config struct {
	LogLevel int8 `usage:"(0 = debug, 1 = info, 2 = warn, 3 = error, 4 = fatal, 5 = panic)"`
	// WatchDirs is the list of directories monitored by metad for file-open events.
	WatchDirs []string `usage:"list of directories to watch for file-open events"`
	// DaemonDB is the database configuration for the application used by both metad and metafilter.
	// stores file-open events detected by metad and read by metafilter at print time.
	DaemonDB DatabaseConfig `usage:"database configuration for the daemon"`
	// DatabaseDir is the directory that contains the SQLite databases read by metafilter at print time.
	DatabaseDir string `usage:"directory that contains the SQLite databases read by metafilter at print time"`
}

type DatabaseConfig struct {
	Driver   string `usage:"Database driver, (sqlite3, mysql, mariadb)"`
	Host     string `usage:"IP address or hostname of the database server"`
	Port     uint16 `usage:"Port of the database server to connect to"`
	User     string `usage:"Database username"`
	Password string `usage:"Database password"`
	Name     string `usage:"Name of the database or sqlite file"`
}

func Init() {
	home, _ := os.UserHomeDir()

	// create an instance of the configManager
	cm := config.Manager()
	cm.WithName("meta-printer")
	conf := &Config{
		LogLevel:  -1,
		WatchDirs: defaultWatchDirs(home),

		DaemonDB: DatabaseConfig{
			Driver: "sqlite",
			Host:   "localhost",
			Port:   3306,
			Name:   "metadata",
		},
		DatabaseDir: DefaultDatabaseDir,
	}

	err := cm.Register(conf)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to register config")
	}

	flag.Init()

	err = config.Read()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read config")
	}
}

// defaultWatchDirs returns the candidate watch directories that exist under
// the given home directory.
func defaultWatchDirs(home string) []string {
	if home == "" {
		return nil
	}
	// set up default candidate directories to watch
	candidates := []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Desktop"),
	}
	// check whether the candidate directories exist
	var existing []string
	for _, d := range candidates {
		if _, err := os.Stat(d); err == nil {
			existing = append(existing, d)
		}
	}
	return existing
}

func Get() Config {
	conf, ok := config.Get().(*Config)
	if !ok {
		return Config{}
	}

	if conf == nil {
		return Config{}
	}

	return *conf
}

func Write(change *Config) error {
	return apperror.Wrap(config.Write(change))
}

func (c Config) Validate() error {
	if c.LogLevel < -1 || c.LogLevel > 5 {
		return apperror.NewError("LogLevel must be between -1 and 5")
	}
	if err := c.DaemonDB.Validate(); err != nil {
		return apperror.Wrap(err)
	}

	if c.DatabaseDir == "" {
		return apperror.NewError("DatabaseDir must be provided")
	}
	return nil
}

// ValidateWatch checks the settings only metad needs. metafilter must not
// call it, as it runs without a home directory and thus without watch dirs.
func (c Config) ValidateWatch() error {
	if len(c.WatchDirs) == 0 {
		return apperror.NewError("WatchDirs can not be empty")
	}
	return nil
}

func (dc DatabaseConfig) Validate() error {
	if dc.Driver == "" {
		return apperror.NewError("Database driver must be provided")
	}

	switch dc.Driver {
	case "sqlite":
		if dc.Name == "" {
			return apperror.NewError("database name (sqlite file) is required")
		}
	default:
		if dc.Host == "" {
			return apperror.NewError("database host is required")
		}
		if dc.Port == 0 {
			return apperror.NewError("database port is required")
		}
		if dc.User == "" {
			return apperror.NewError("database user is required")
		}
		if dc.Password == "" {
			return apperror.NewError("database password is required")
		}
		if dc.Name == "" {
			return apperror.NewError("database name is required")
		}
	}
	return nil
}
