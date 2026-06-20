package models

import (
	"time"

	"gin-server/database"
)

type Group struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	OwnerID   int       `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateGroup makes a new group and adds the owner as its first member.
func CreateGroup(name string, ownerID int) (*Group, error) {
	res, err := database.DB.Exec(
		"INSERT INTO groups (name, owner_id) VALUES (?, ?)", name, ownerID,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := database.DB.Exec(
		"INSERT OR IGNORE INTO group_members (group_id, user_id) VALUES (?, ?)", id, ownerID,
	); err != nil {
		return nil, err
	}
	return GetGroupByID(int(id))
}

func GetGroupByID(id int) (*Group, error) {
	g := &Group{}
	err := database.DB.QueryRow(
		"SELECT id, name, owner_id, created_at FROM groups WHERE id = ?", id,
	).Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetGroupsForUser returns every group the user belongs to.
func GetGroupsForUser(userID int) ([]*Group, error) {
	rows, err := database.DB.Query(`
		SELECT g.id, g.name, g.owner_id, g.created_at
		FROM groups g
		JOIN group_members m ON m.group_id = g.id
		WHERE m.user_id = ?
		ORDER BY g.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []*Group
	for rows.Next() {
		g := &Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func AddGroupMember(groupID, userID int) error {
	_, err := database.DB.Exec(
		"INSERT OR IGNORE INTO group_members (group_id, user_id) VALUES (?, ?)", groupID, userID,
	)
	return err
}

func IsGroupMember(groupID, userID int) (bool, error) {
	var n int
	err := database.DB.QueryRow(
		"SELECT COUNT(*) FROM group_members WHERE group_id = ? AND user_id = ?", groupID, userID,
	).Scan(&n)
	return n > 0, err
}

// GetGroupMembers returns the approved users in a group.
func GetGroupMembers(groupID int) ([]*User, error) {
	return queryUsers(`
		SELECT `+userCols+`
		FROM users u
		JOIN group_members m ON m.user_id = u.id
		WHERE m.group_id = ?
		ORDER BY u.username`, groupID)
}
