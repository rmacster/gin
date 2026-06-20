package database

import (
	"database/sql"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Initialize(dbPath string) error {
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	// Serialize access through a single connection and wait on locks rather than
	// erroring, so concurrent reads (auth) and writes (game-state saves) don't
	// collide. WAL also improves read/write concurrency.
	DB.SetMaxOpenConns(1)
	if err = DB.Ping(); err != nil {
		return err
	}
	if _, err = DB.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	if err = createTables(); err != nil {
		return err
	}
	// Defensive migrations for databases created before these columns existed.
	DB.Exec("ALTER TABLE games ADD COLUMN game_type TEXT DEFAULT 'gin'")
	DB.Exec("ALTER TABLE games ADD COLUMN state TEXT DEFAULT NULL")
	if err = createDefaultAdmin(); err != nil {
		return err
	}
	return nil
}

func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',   -- 'user' | 'admin'
		is_robot BOOLEAN DEFAULT FALSE,
		approved BOOLEAN DEFAULT FALSE,
		games_played INTEGER DEFAULT 0,
		wins INTEGER DEFAULT 0,
		losses INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		owner_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (owner_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS group_members (
		group_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		PRIMARY KEY (group_id, user_id),
		FOREIGN KEY (group_id) REFERENCES groups(id),
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS games (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		group_id INTEGER DEFAULT NULL,
		status TEXT DEFAULT 'active',     -- 'active' | 'finished'
		game_type TEXT DEFAULT 'gin',     -- 'gin' | 'rummy'
		winner_id INTEGER DEFAULT NULL,
		target_score INTEGER DEFAULT 100,
		state TEXT DEFAULT NULL,          -- serialized live engine state (for resume across reboots)
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME DEFAULT NULL
	);

	CREATE TABLE IF NOT EXISTS game_players (
		game_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		seat INTEGER NOT NULL,
		PRIMARY KEY (game_id, user_id),
		FOREIGN KEY (game_id) REFERENCES games(id),
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_user_id INTEGER NOT NULL,
		game_id INTEGER DEFAULT NULL,
		group_id INTEGER DEFAULT NULL,
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (from_user_id) REFERENCES users(id)
	);
	`
	_, err := DB.Exec(schema)
	return err
}

func createDefaultAdmin() error {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		password = "gin2024" // local-dev default; set ADMIN_PASSWORD in production
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = DB.Exec(
		"INSERT INTO users (username, email, password_hash, role, approved) VALUES (?, ?, ?, 'admin', TRUE)",
		"admin", "admin@gin.local", string(hash),
	)
	if err != nil {
		log.Printf("Could not create default admin: %v", err)
	} else if os.Getenv("ADMIN_PASSWORD") == "" {
		log.Println("Created default admin user (admin / gin2024) — set ADMIN_PASSWORD for production")
	} else {
		log.Println("Created default admin user (admin) with the password from ADMIN_PASSWORD")
	}
	return nil
}

// RunMigrations is a no-op placeholder for forward compatibility.
func RunMigrations() error { return nil }
