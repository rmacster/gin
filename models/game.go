package models

import (
	"database/sql"
	"time"

	"gin-server/database"
)

type GameRow struct {
	ID          int       `json:"id"`
	GroupID     *int      `json:"group_id"`
	Status      string    `json:"status"`
	GameType    string    `json:"game_type"`
	WinnerID    *int      `json:"winner_id"`
	TargetScore int       `json:"target_score"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateGameRow records a new game and returns its id. groupID may be nil.
func CreateGameRow(groupID *int, gameType string, target int) (int, error) {
	res, err := database.DB.Exec(
		"INSERT INTO games (group_id, game_type, target_score, status) VALUES (?, ?, ?, 'active')",
		groupID, gameType, target,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func AddGamePlayer(gameID, userID, seat int) error {
	_, err := database.DB.Exec(
		"INSERT OR IGNORE INTO game_players (game_id, user_id, seat) VALUES (?, ?, ?)",
		gameID, userID, seat,
	)
	return err
}

// FinishGame marks a game finished and records the winner.
func FinishGame(gameID, winnerID int) error {
	_, err := database.DB.Exec(
		"UPDATE games SET status = 'finished', winner_id = ?, ended_at = CURRENT_TIMESTAMP WHERE id = ?",
		winnerID, gameID,
	)
	return err
}

// RemoveGamePlayer drops a player's seat row (e.g. when they decline an invite).
func RemoveGamePlayer(gameID, userID int) error {
	_, err := database.DB.Exec(
		"DELETE FROM game_players WHERE game_id = ? AND user_id = ?", gameID, userID,
	)
	return err
}

// IsGamePlayer reports whether the user is/was seated in the game.
func IsGamePlayer(gameID, userID int) (bool, error) {
	var n int
	err := database.DB.QueryRow(
		"SELECT COUNT(*) FROM game_players WHERE game_id = ? AND user_id = ?", gameID, userID,
	).Scan(&n)
	return n > 0, err
}

// DeleteGame removes a game and its associated rows (players, chat).
func DeleteGame(gameID int) error {
	database.DB.Exec("DELETE FROM messages WHERE game_id = ?", gameID)
	database.DB.Exec("DELETE FROM game_players WHERE game_id = ?", gameID)
	_, err := database.DB.Exec("DELETE FROM games WHERE id = ?", gameID)
	return err
}

// SaveGameState persists the serialized live engine state for resume-after-reboot.
func SaveGameState(gameID int, state []byte) error {
	_, err := database.DB.Exec("UPDATE games SET state = ? WHERE id = ?", string(state), gameID)
	return err
}

// ActiveGame is a persisted, still-active game that can be restored into the hub.
type ActiveGame struct {
	ID       int
	GameType string
	State    []byte
}

// GetActiveGames returns every active game that has saved state, for restore on startup.
func GetActiveGames() ([]ActiveGame, error) {
	rows, err := database.DB.Query(
		"SELECT id, game_type, state FROM games WHERE status = 'active' AND state IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActiveGame
	for rows.Next() {
		var g ActiveGame
		var state string
		if err := rows.Scan(&g.ID, &g.GameType, &state); err != nil {
			return nil, err
		}
		g.State = []byte(state)
		out = append(out, g)
	}
	return out, nil
}

// GetGamesForUser returns the games a user is (or was) seated in, newest first.
func GetGamesForUser(userID int) ([]*GameRow, error) {
	rows, err := database.DB.Query(`
		SELECT g.id, g.group_id, g.status, g.game_type, g.winner_id, g.target_score, g.created_at
		FROM games g
		JOIN game_players p ON p.game_id = g.id
		WHERE p.user_id = ?
		ORDER BY g.created_at DESC
		LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GameRow
	for rows.Next() {
		g := &GameRow{}
		var grp, win sql.NullInt64
		if err := rows.Scan(&g.ID, &grp, &g.Status, &g.GameType, &win, &g.TargetScore, &g.CreatedAt); err != nil {
			return nil, err
		}
		if grp.Valid {
			v := int(grp.Int64)
			g.GroupID = &v
		}
		if win.Valid {
			v := int(win.Int64)
			g.WinnerID = &v
		}
		out = append(out, g)
	}
	return out, nil
}
