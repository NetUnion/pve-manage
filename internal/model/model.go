package model

import "time"

type User struct {
	ID          int64
	Username    string
	Email       string
	Name        string
	GroupsJSON  string
	IsActive    bool
	IsAdmin     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt *time.Time
}

type VM struct {
	ID                  int64
	OwnerUsername       string
	ClusterKey          string
	VMID                int
	VMName              string
	IP                  string
	Node                string
	Password            string
	SSHKeysJSON         string
	SharedUsernamesJSON string
	SecurityGroupName   string
	UESTCRestricted     bool
	ConfigJSON          string
	PreferStatusJSON    string
	RealStatusJSON      string
	SyncState           string
	SyncError           *string
	Version             int
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
	DeleteRequestedAt   *time.Time
	DeleteExecuteAfter  *time.Time
}

type SecurityGroup struct {
	ID            int64
	OwnerUsername string
	Name          string
	RulesJSON     string
	PolicyIn      string
	PolicyOut     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Template struct {
	ID             int64
	ClusterKey     string
	TemplateVMID   int
	Name           string
	Description    *string
	OSType         *string
	RealStatusJSON string
	LastSeenAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
