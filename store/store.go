package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// Store encapsulates database operations.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store instance.
func NewStore(dataSourceName string) (*Store, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() {
	s.db.Close()
}

// InitSchema initializes the database schema.
func (s *Store) InitSchema() {
	createContainerTable := `
	CREATE TABLE IF NOT EXISTS selected_containers (
		id TEXT PRIMARY KEY
	);`
	if _, err := s.db.Exec(createContainerTable); err != nil {
		log.Fatalf("Failed to create selected_containers table: %s", err)
	}

	createVolumeTable := `
	CREATE TABLE IF NOT EXISTS selected_volumes (
		name TEXT PRIMARY KEY
	);`
	if _, err := s.db.Exec(createVolumeTable); err != nil {
		log.Fatalf("Failed to create selected_volumes table: %s", err)
	}
}

// GetSelectedContainers retrieves a map of selected container IDs.
func (s *Store) GetSelectedContainers() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT id FROM selected_containers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	selected := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		selected[id] = true
	}
	return selected, nil
}

// GetSelectedVolumes retrieves a map of selected volume names.
func (s *Store) GetSelectedVolumes() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT name FROM selected_volumes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	selected := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		selected[name] = true
	}
	return selected, nil
}

// UpdateSelection updates the selection state for a container or volume.
func (s *Store) UpdateSelection(itemType, id, name string, isSelected bool) error {
	var query string
	var args []interface{}

	if itemType == "container" {
		if isSelected {
			query = "INSERT OR IGNORE INTO selected_containers (id) VALUES (?)"
			args = append(args, id)
		} else {
			query = "DELETE FROM selected_containers WHERE id = ?"
			args = append(args, id)
		}
	} else if itemType == "volume" {
		if isSelected {
			query = "INSERT OR IGNORE INTO selected_volumes (name) VALUES (?)"
			args = append(args, name)
		} else {
			query = "DELETE FROM selected_volumes WHERE name = ?"
			args = append(args, name)
		}
	} else {
		return fmt.Errorf("invalid selection type: %s", itemType)
	}

	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("database operation failed: %w", err)
	}
	return nil
}
