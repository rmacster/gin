package models

import (
	"time"

	"gin-server/database"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Approved     bool      `json:"approved"`
	GamesPlayed  int       `json:"games_played"`
	Wins         int       `json:"wins"`
	Losses       int       `json:"losses"`
	CreatedAt    time.Time `json:"created_at"`
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

const userCols = `id, username, email, password_hash, role, approved, games_played, wins, losses, created_at`

func scanUser(s interface{ Scan(...interface{}) error }) (*User, error) {
	u := &User{}
	err := s.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role,
		&u.Approved, &u.GamesPlayed, &u.Wins, &u.Losses, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func CreateUser(username, email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := database.DB.Exec(
		"INSERT INTO users (username, email, password_hash, role, approved) VALUES (?, ?, ?, 'user', FALSE)",
		username, email, string(hash),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetUserByID(int(id))
}

func GetUserByID(id int) (*User, error) {
	return scanUser(database.DB.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func GetUserByUsername(username string) (*User, error) {
	return scanUser(database.DB.QueryRow("SELECT "+userCols+" FROM users WHERE username = ?", username))
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

func queryUsers(q string, args ...interface{}) ([]*User, error) {
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func GetPendingUsers() ([]*User, error) {
	return queryUsers("SELECT " + userCols + " FROM users WHERE approved = FALSE AND role = 'user' ORDER BY created_at")
}

func GetAllUsers() ([]*User, error) {
	return queryUsers("SELECT " + userCols + " FROM users ORDER BY role DESC, username")
}

// GetActivePlayers returns approved, non-admin users (the people who can play).
func GetActivePlayers() ([]*User, error) {
	return queryUsers("SELECT " + userCols + " FROM users WHERE approved = TRUE AND role = 'user' ORDER BY username")
}

func ApproveUser(userID int) error {
	_, err := database.DB.Exec("UPDATE users SET approved = TRUE WHERE id = ? AND role = 'user'", userID)
	return err
}

func RejectUser(userID int) error {
	_, err := database.DB.Exec("DELETE FROM users WHERE id = ? AND approved = FALSE AND role = 'user'", userID)
	return err
}

func DeleteUser(userID int) error {
	var role string
	if err := database.DB.QueryRow("SELECT role FROM users WHERE id = ?", userID).Scan(&role); err != nil {
		return err
	}
	if role == "admin" {
		return nil
	}
	database.DB.Exec("DELETE FROM messages WHERE from_user_id = ?", userID)
	database.DB.Exec("DELETE FROM group_members WHERE user_id = ?", userID)
	database.DB.Exec("DELETE FROM groups WHERE owner_id = ?", userID)
	_, err := database.DB.Exec("DELETE FROM users WHERE id = ? AND role = 'user'", userID)
	return err
}

func RecordResult(userID int, win bool) error {
	w := 0
	l := 1
	if win {
		w, l = 1, 0
	}
	_, err := database.DB.Exec(
		"UPDATE users SET games_played = games_played + 1, wins = wins + ?, losses = losses + ? WHERE id = ?",
		w, l, userID,
	)
	return err
}
