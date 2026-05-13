package server

type MyfilesPermissions struct {
	FilesRead       bool `json:"filesRead"`
	FilesWrite      bool `json:"filesWrite"`
	AllFilesRead    bool `json:"allFilesRead"`
	AllFilesWrite   bool `json:"allFilesWrite"`
	BatchesRead     bool `json:"batchesRead"`
	BatchesWrite    bool `json:"batchesWrite"`
	AuditRead       bool `json:"auditRead"`
	SettingsRead    bool `json:"settingsRead"`
	SettingsWrite   bool `json:"settingsWrite"`
	StorageSettings bool `json:"storageSettings"`
	CDNSettings     bool `json:"cdnSettings"`
}

func derivePermissions(role, userType string, capabilities map[string]any) MyfilesPermissions {
	r := role
	if r == "" {
		r = userType
	}
	switch r {
	case "system_admin":
		return MyfilesPermissions{
			FilesRead: true, FilesWrite: true, AllFilesRead: true, AllFilesWrite: true,
			BatchesRead: true, BatchesWrite: true, AuditRead: true,
			SettingsRead: true, SettingsWrite: true, StorageSettings: true, CDNSettings: true,
		}
	case "operator":
		return MyfilesPermissions{
			FilesRead: true, FilesWrite: true, AllFilesRead: true, AllFilesWrite: true,
			BatchesRead: true, BatchesWrite: true,
		}
	case "auditor":
		return MyfilesPermissions{FilesRead: true, AllFilesRead: true, BatchesRead: true, AuditRead: true, SettingsRead: true}
	default:
		return MyfilesPermissions{FilesRead: true, FilesWrite: true}
	}
}
