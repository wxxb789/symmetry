package linuxpreflight

// Closed decision vocabulary of the AppArmor unprivileged-userns host precondition; a refusal
// is the decision line of the returned error and never carries a path, errno or free text.
const (
	LinuxAppArmorUsernsNotNeeded       = "not_needed"
	LinuxAppArmorUsernsPerProgram      = "per_program"
	LinuxAppArmorUsernsSysctlInvalid   = "sysctl_invalid"
	LinuxAppArmorUsernsStateUnreadable = "apparmor_state_unreadable"
	LinuxAppArmorUsernsUnexpected      = "unexpected_profile"
	LinuxAppArmorUsernsAbsent          = "profile_absent"
	LinuxAppArmorUsernsFileMismatch    = "profile_file_mismatch"
	LinuxAppArmorUsernsNotLoaded       = "profile_not_loaded"
	LinuxAppArmorUsernsLoadedMismatch  = "profile_loaded_mismatch"
	LinuxAppArmorUsernsParserAbsent    = "parser_absent"
	LinuxAppArmorUsernsParserFailed    = "parser_failed"
)
