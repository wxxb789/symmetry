//go:build linux && amd64

package linuxpreflight

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

func verifyLinuxSystemdUnits(value config.Config, accounts OwnerAccounts) error {
	group, err := user.LookupGroupId(accounts.a.Gid)
	if err != nil {
		return fmt.Errorf("lookup A systemd service group: %w", err)
	}
	delegatedSubgroup := filepath.Base(value.LinuxOwnerDelegatedPath)
	daemonPolicy := linuxSystemdUnitPolicy{
		unit: "symmetry-daemon.service", user: accounts.a.Username, group: group.Name, home: accounts.a.HomeDir,
		execStart:    []string{"/usr/libexec/symmetry/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"},
		execStartPre: []string{"/usr/libexec/symmetry/symmetry-daemon", "-verify-linux-owner-install", "-config", "/etc/symmetry/daemon.json"},
	}
	ownerPolicy := linuxSystemdUnitPolicy{
		unit: "symmetry-owner.service", user: accounts.a.Username, group: group.Name, home: accounts.a.HomeDir,
		owner: true, delegate: "yes", delegatedSubgroup: delegatedSubgroup,
		execStart: []string{"/usr/libexec/symmetry/symmetry-daemon", "-linux-owner-anchor", "-cgroup-mount", value.LinuxOwnerCgroupMount,
			"-delegated-path", value.LinuxOwnerDelegatedPath, "-socket", value.LinuxOwnerSocket},
		execStop: []string{"/usr/libexec/symmetry/symmetry-daemon", "-recover-containment-only", "-config", "/etc/symmetry/daemon.json"},
	}
	for _, policy := range []linuxSystemdUnitPolicy{daemonPolicy, ownerPolicy} {
		if err := verifyLinuxSystemdUnit(policy, accounts.ASubject, accounts.BSubject); err != nil {
			return err
		}
	}
	return nil
}

type linuxSystemdUnitPolicy struct {
	unit              string
	user              string
	group             string
	home              string
	owner             bool
	delegate          string
	delegatedSubgroup string
	execStart         []string
	execStartPre      []string
	execStop          []string
}

func verifyLinuxSystemdUnit(policy linuxSystemdUnitPolicy, a, b ACLSubject) error {
	path := filepath.Join("/etc/systemd/system", policy.unit)
	transientDirectory := "/run/systemd/transient"
	if err := verifyLinuxSystemdDirectory("/run/systemd", a, b); err != nil {
		return fmt.Errorf("verify transient-unit ancestor: %w", err)
	}
	if _, err := LstatCanonicalPath(transientDirectory); err == nil {
		if err := verifyLinuxSystemdDirectory(transientDirectory, a, b); err != nil {
			return fmt.Errorf("verify transient-unit directory: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect transient-unit directory: %w", err)
	}
	if _, err := LstatCanonicalPath(filepath.Join(transientDirectory, policy.unit)); err == nil {
		return fmt.Errorf("transient unit overrides installed %s", policy.unit)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect transient unit override for %s: %w", policy.unit, err)
	}
	if err := verifyLinuxSystemdDirectory(filepath.Dir(path), a, b); err != nil {
		return fmt.Errorf("verify parent of staged %s unit: %w", policy.unit, err)
	}
	directives, err := readLinuxSystemdDirectives(path, a, b)
	if err != nil {
		return fmt.Errorf("verify staged %s unit: %w", policy.unit, err)
	}
	if err := validateLinuxSystemdCandidate(directives, policy, a, b); err != nil {
		return fmt.Errorf("staged %s unit violates the deployment contract: %w", policy.unit, err)
	}
	var validatedDropIns []string
	for _, directory := range linuxSystemdDropInDirectories(policy.unit) {
		present, err := verifyLinuxOptionalSystemdDirectory(directory, a, b)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return fmt.Errorf("list %s drop-ins read-only: %w", policy.unit, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
				if entry.IsDir() {
					return fmt.Errorf("unexpected nested directory in %s drop-ins: %s", policy.unit, entry.Name())
				}
				continue
			}
			dropIn := filepath.Join(directory, entry.Name())
			directives, err := readLinuxSystemdDirectives(dropIn, a, b)
			if err != nil {
				return fmt.Errorf("verify %s drop-in %s: %w", policy.unit, entry.Name(), err)
			}
			if err := validateLinuxSystemdOverrides(directives, policy, a, b); err != nil {
				return fmt.Errorf("%s drop-in %s weakens the owner boundary: %w", policy.unit, entry.Name(), err)
			}
			validatedDropIns = append(validatedDropIns, dropIn)
		}
	}
	return verifyEffectiveLinuxSystemdUnit(policy, validatedDropIns, a, b)
}

func verifyEffectiveLinuxSystemdUnit(policy linuxSystemdUnitPolicy, validatedDropIns []string, a, b ACLSubject) error {
	propertiesToShow := []string{
		"LoadState", "FragmentPath", "DropInPaths", "ActiveState", "SubState", "ControlGroup",
		"Type", "NotifyAccess", "User", "Group", "NoNewPrivileges", "UMask", "WorkingDirectory",
		"KillMode", "SendSIGKILL", "TimeoutStopUSec", "Restart",
		"ExecStart", "ExecStartPre", "ExecStop", "ExecStopPost", "Slice", "RuntimeDirectory",
		"RuntimeDirectoryMode", "Delegate", "DelegateSubgroup", "After", "Before", "BindsTo",
		"Requires", "Requisite", "Wants", "PartOf", "Conflicts",
		"RequiresMountsFor", "Environment", "EnvironmentFiles", "PassEnvironment", "UnsetEnvironment",
		"SupplementaryGroups",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", "--all", "--no-pager", "--property="+strings.Join(propertiesToShow, ","), policy.unit)
	command.Env = []string{"HOME=/", "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=cat"}
	var stdout, stderr boundedLinuxOutput
	stdout.limit = linuxSystemdShowMaxBytes
	stderr.limit = linuxSystemdShowMaxBytes
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("effective systemd show for %s is unavailable: %w", policy.unit, ctx.Err())
		}
		if stdout.tooLarge || stderr.tooLarge || errors.Is(err, errLinuxPreflightOutputTooLarge) {
			return fmt.Errorf("effective systemd show for %s exceeded its bounded output", policy.unit)
		}
		return fmt.Errorf("query effective systemd properties for %s: %w: %s", policy.unit, err, strings.TrimSpace(stderr.String()))
	}
	properties, err := parseLinuxSystemdShowProperties(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("parse effective systemd properties for %s: %w", policy.unit, err)
	}
	if err := validateEffectiveLinuxSystemdProperties(properties, policy, validatedDropIns, a, b); err != nil {
		return fmt.Errorf("effective %s unit violates the owner service contract: %w", policy.unit, err)
	}
	return nil
}

func parseLinuxSystemdShowProperties(contents []byte) (map[string]string, error) {
	properties := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxSystemdShowMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("systemd show line %d is not a property assignment", lineNumber)
		}
		key = strings.TrimSpace(key)
		if _, exists := properties[key]; exists {
			return nil, fmt.Errorf("systemd show repeats property %s", key)
		}
		properties[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return properties, nil
}

func validateEffectiveLinuxSystemdProperties(properties map[string]string, policy linuxSystemdUnitPolicy, validatedDropIns []string, a, b ACLSubject) error {
	for _, key := range []string{"LoadState", "FragmentPath", "DropInPaths", "Type", "User", "Group", "NoNewPrivileges", "UMask", "WorkingDirectory",
		"KillMode", "SendSIGKILL", "TimeoutStopUSec", "Restart", "ExecStart", "ExecStartPre",
		"ExecStop", "ExecStopPost", "Delegate", "DelegateSubgroup", "After", "Before", "BindsTo", "Requires", "Requisite",
		"Wants", "PartOf", "Conflicts", "RequiresMountsFor", "Environment", "EnvironmentFiles",
		"PassEnvironment", "UnsetEnvironment", "SupplementaryGroups"} {
		if _, ok := properties[key]; !ok {
			// systemctl omits unset properties. Missing is equivalent to an
			// empty value; required non-empty values still fail their checks.
			properties[key] = ""
		}
	}
	fragment := filepath.Join("/etc/systemd/system", policy.unit)
	if properties["LoadState"] != "loaded" || properties["FragmentPath"] != fragment {
		return fmt.Errorf("effective fragment is %q with LoadState %q, require the installed %s unit", properties["FragmentPath"], properties["LoadState"], fragment)
	}
	if err := verifyLinuxSystemdDropInList(properties["DropInPaths"], validatedDropIns); err != nil {
		return err
	}
	for key, expected := range map[string]string{
		"User": policy.user, "Group": policy.group, "NoNewPrivileges": "no", "UMask": "0077",
		"WorkingDirectory": "/", "KillMode": "control-group", "SendSIGKILL": "yes", "Restart": "on-failure",
	} {
		if !strings.EqualFold(properties[key], expected) {
			return fmt.Errorf("effective %s=%q, require %q", key, properties[key], expected)
		}
	}
	stopDuration := uint64(120_000_000)
	if policy.owner {
		stopDuration = 720_000_000
	}
	if err := requireLinuxSystemdShowDuration(properties, "TimeoutStopUSec", stopDuration); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStart", policy.execStart); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStartPre", policy.execStartPre); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStop", policy.execStop); err != nil {
		return err
	}
	if err := requireLinuxSystemdShowExec(properties, "ExecStopPost", nil); err != nil {
		return err
	}
	if !systemdListHasItem([]string{properties["After"]}, "local-fs.target") {
		return errors.New("effective unit ordering omits local-fs.target")
	}
	if policy.owner {
		for _, key := range []string{"Before", "After", "BindsTo", "Requires", "Requisite", "Wants", "PartOf", "Conflicts"} {
			if systemdListHasItem([]string{properties[key]}, "symmetry-daemon.service") {
				return fmt.Errorf("effective owner unit depends on or orders against symmetry-daemon.service through %s", key)
			}
		}
		for key, expected := range map[string]string{
			"Type": "notify", "NotifyAccess": "main", "Slice": "system.slice", "RuntimeDirectory": "symmetry-owner",
			"RuntimeDirectoryMode": "0700", "Delegate": "yes", "DelegateSubgroup": policy.delegatedSubgroup,
			"ControlGroup": "/system.slice/symmetry-owner.service", "ActiveState": "active", "SubState": "running",
		} {
			if properties[key] != expected {
				return fmt.Errorf("effective owner %s=%q, require %q", key, properties[key], expected)
			}
		}
		if !systemdListHasItem([]string{properties["RequiresMountsFor"]}, "/sys/fs/cgroup") {
			return errors.New("effective owner unit omits RequiresMountsFor=/sys/fs/cgroup")
		}
	} else {
		for key, expected := range map[string]string{"Type": "simple", "Delegate": "no"} {
			if !strings.EqualFold(properties[key], expected) {
				return fmt.Errorf("effective daemon %s=%q, require %q", key, properties[key], expected)
			}
		}
		if properties["DelegateSubgroup"] != "" || properties["RuntimeDirectory"] != "" {
			return errors.New("effective daemon unit must not own a delegated/runtime directory")
		}
		for _, key := range []string{"After", "BindsTo"} {
			if !systemdListHasItem([]string{properties[key]}, "symmetry-owner.service") {
				return fmt.Errorf("effective daemon %s omits symmetry-owner.service", key)
			}
		}
	}
	effectiveEnvironment := make(map[string][]string, 4)
	for property, key := range map[string]string{
		"Environment": "environment", "EnvironmentFiles": "environmentfile", "PassEnvironment": "passenvironment",
		"UnsetEnvironment": "unsetenvironment",
	} {
		if value := properties[property]; value != "" {
			effectiveEnvironment[key] = []string{value}
		}
	}
	if err := validateLinuxSystemdEnvironment(effectiveEnvironment, policy.home, a, b); err != nil {
		return fmt.Errorf("effective environment can alter trusted Git/credential behavior: %w", err)
	}
	if strings.TrimSpace(properties["SupplementaryGroups"]) != "" {
		return errors.New("effective SupplementaryGroups adds host groups to A")
	}
	if strings.TrimSpace(properties["EnvironmentFiles"]) != "" || strings.TrimSpace(properties["PassEnvironment"]) != "" || strings.TrimSpace(properties["UnsetEnvironment"]) != "" {
		return errors.New("effective unit has an EnvironmentFile/PassEnvironment/UnsetEnvironment override")
	}
	return nil
}

func requireLinuxSystemdShowDuration(properties map[string]string, key string, expected uint64) error {
	actual, err := parseLinuxSystemdDurationMicroseconds(properties[key])
	if err != nil || actual != expected {
		return fmt.Errorf("effective %s=%q, require exactly %d microseconds", key, properties[key], expected)
	}
	return nil
}

func parseLinuxSystemdDurationMicroseconds(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty systemd duration")
	}
	if raw, err := strconv.ParseUint(value, 10, 64); err == nil {
		return raw, nil
	}
	fields := strings.Fields(value)
	var total uint64
	for _, field := range fields {
		index := 0
		for index < len(field) && field[index] >= '0' && field[index] <= '9' {
			index++
		}
		if index == 0 || index == len(field) {
			return 0, fmt.Errorf("unsupported systemd duration component %q", field)
		}
		amount, err := strconv.ParseUint(field[:index], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid systemd duration component %q", field)
		}
		var multiplier uint64
		switch field[index:] {
		case "h":
			multiplier = 3_600_000_000
		case "min":
			multiplier = 60_000_000
		case "s":
			multiplier = 1_000_000
		case "ms":
			multiplier = 1_000
		case "us", "µs":
			multiplier = 1
		case "ns":
			if amount%1_000 != 0 {
				return 0, errors.New("systemd nanosecond duration is not an exact microsecond")
			}
			amount /= 1_000
			multiplier = 1
		default:
			return 0, fmt.Errorf("unsupported systemd duration unit %q", field[index:])
		}
		if amount > ^uint64(0)/multiplier {
			return 0, errors.New("systemd duration overflows microseconds")
		}
		component := amount * multiplier
		if total > ^uint64(0)-component {
			return 0, errors.New("systemd duration overflows microseconds")
		}
		total += component
	}
	return total, nil
}

func requireLinuxSystemdShowExec(properties map[string]string, key string, expected []string) error {
	actual, err := parseLinuxSystemdShowExec(properties[key])
	if err != nil {
		return fmt.Errorf("parse effective %s: %w", key, err)
	}
	if len(expected) == 0 {
		if len(actual.argv) != 0 {
			return fmt.Errorf("effective %s unexpectedly contains argv %q", key, actual.argv)
		}
		return nil
	}
	if actual.flags != "" || actual.ignoreErrors != "no" {
		return fmt.Errorf("effective %s has unsafe flags=%q ignore_errors=%q", key, actual.flags, actual.ignoreErrors)
	}
	if len(actual.argv) != len(expected) {
		return fmt.Errorf("effective %s argv is %q, require %q", key, actual.argv, expected)
	}
	for index := range expected {
		if actual.argv[index] != expected[index] {
			return fmt.Errorf("effective %s argv is %q, require %q", key, actual.argv, expected)
		}
	}
	return nil
}

type linuxSystemdExecProperty struct {
	argv         []string
	flags        string
	ignoreErrors string
}

func parseLinuxSystemdShowExec(value string) (linuxSystemdExecProperty, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "{}" || value == "[]" {
		return linuxSystemdExecProperty{}, nil
	}
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") || strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec property is not one command object")
	}
	var path, argv, flags, ignoreErrors string
	pathCount, argvCount, flagsCount, ignoreErrorsCount := 0, 0, 0, 0
	for _, part := range strings.Split(strings.TrimSpace(value[1:len(value)-1]), " ; ") {
		key, content, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			return linuxSystemdExecProperty{}, errors.New("systemd Exec property contains a malformed command field")
		}
		switch key {
		case "path":
			path, pathCount = content, pathCount+1
		case "argv[]":
			argv, argvCount = content, argvCount+1
		case "flags":
			flags, flagsCount = content, flagsCount+1
		case "ignore_errors":
			ignoreErrors, ignoreErrorsCount = content, ignoreErrorsCount+1
		}
	}
	if pathCount != 1 || argvCount != 1 || flagsCount > 1 || ignoreErrorsCount > 1 || path == "" || argv == "" {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec property lacks unique path and argv[] fields")
	}
	if ignoreErrorsCount == 0 {
		ignoreErrors = "no"
	}
	arguments := strings.Fields(argv)
	if len(arguments) == 0 || arguments[0] != path {
		return linuxSystemdExecProperty{}, errors.New("systemd Exec path differs from argv[0]")
	}
	return linuxSystemdExecProperty{argv: arguments, flags: flags, ignoreErrors: ignoreErrors}, nil
}

func verifyLinuxSystemdDropInList(actualValue string, expectedPaths []string) error {
	actualFields := strings.Fields(strings.TrimSpace(actualValue))
	actual := make(map[string]struct{}, len(actualFields))
	for _, path := range actualFields {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("effective systemd DropInPaths contains noncanonical path %q", path)
		}
		if _, exists := actual[path]; exists {
			return fmt.Errorf("effective systemd DropInPaths repeats %q", path)
		}
		actual[path] = struct{}{}
	}
	expected := make(map[string]struct{}, len(expectedPaths))
	for _, path := range expectedPaths {
		expected[path] = struct{}{}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("effective systemd DropInPaths are %q, expected exactly %q", actualFields, expectedPaths)
	}
	for path := range expected {
		if _, ok := actual[path]; !ok {
			return fmt.Errorf("effective systemd DropInPaths omit the validated file %s", path)
		}
	}
	return nil
}

func validateLinuxSystemdCandidate(directives map[string][]string, policy linuxSystemdUnitPolicy, a, b ACLSubject) error {
	common := []struct{ key, value string }{
		{"nonewprivileges", "no"}, {"user", policy.user}, {"group", policy.group}, {"umask", "0077"},
		{"workingdirectory", "/"}, {"killmode", "control-group"}, {"sendsigkill", "yes"},
		{"timeoutstartsec", "30s"}, {"restart", "on-failure"}, {"restartsec", "5s"},
	}
	for _, directive := range common {
		if err := requireLinuxSystemdDirective(directives, directive.key, directive.value); err != nil {
			return err
		}
	}
	if len(directives["supplementarygroups"]) != 0 {
		for _, value := range directives["supplementarygroups"] {
			if strings.TrimSpace(value) != "" {
				return errors.New("SupplementaryGroups must not add host groups to A")
			}
		}
	}
	if len(directives["environmentfile"]) != 0 || len(directives["passenvironment"]) != 0 || len(directives["unsetenvironment"]) != 0 {
		return errors.New("EnvironmentFile/PassEnvironment/UnsetEnvironment are not permitted")
	}
	if err := validateLinuxSystemdEnvironment(directives, policy.home, a, b); err != nil {
		return err
	}
	if policy.owner {
		for _, directive := range []struct{ key, value string }{
			{"type", "notify"}, {"notifyaccess", "main"}, {"slice", "system.slice"},
			{"runtimedirectory", "symmetry-owner"}, {"runtimedirectorymode", "0700"},
			{"delegate", policy.delegate}, {"delegatesubgroup", policy.delegatedSubgroup},
			{"timeoutstopsec", "12min"},
		} {
			if err := requireLinuxSystemdDirective(directives, directive.key, directive.value); err != nil {
				return err
			}
		}
		if err := requireLinuxSystemdListExact(directives, "after", []string{"local-fs.target"}); err != nil {
			return err
		}
		if err := requireLinuxSystemdListExact(directives, "requiresmountsfor", []string{"/sys/fs/cgroup"}); err != nil {
			return err
		}
		if err := requireLinuxSystemdExec(directives, "execstart", policy.execStart); err != nil {
			return err
		}
		if err := requireLinuxSystemdExec(directives, "execstop", policy.execStop); err != nil {
			return err
		}
		if len(directives["execstoppost"]) != 0 || len(directives["execstartpre"]) != 0 {
			return errors.New("owner unit must have one recovery-only ExecStop and no ExecStopPost or ExecStartPre")
		}
		if err := rejectLinuxSystemdUnitLists(directives, "before", "bindsto", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess"); err != nil {
			return fmt.Errorf("owner unit has an unexpected reverse/dependent service edge: %w", err)
		}
		return nil
	}
	if err := requireLinuxSystemdDirective(directives, "type", "simple"); err != nil {
		return err
	}
	if err := requireLinuxSystemdDirective(directives, "timeoutstopsec", "2min"); err != nil {
		return err
	}
	if err := requireLinuxSystemdListExact(directives, "after", []string{"local-fs.target", "symmetry-owner.service"}); err != nil {
		return err
	}
	if err := requireLinuxSystemdListExact(directives, "bindsto", []string{"symmetry-owner.service"}); err != nil {
		return err
	}
	if err := rejectLinuxSystemdUnitLists(directives, "before", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess", "requiresmountsfor"); err != nil {
		return fmt.Errorf("daemon unit has an unexpected service dependency edge: %w", err)
	}
	if err := requireLinuxSystemdExec(directives, "execstartpre", policy.execStartPre); err != nil {
		return err
	}
	if err := requireLinuxSystemdExec(directives, "execstart", policy.execStart); err != nil {
		return err
	}
	if len(directives["execstop"]) != 0 || len(directives["execstoppost"]) != 0 {
		return errors.New("daemon unit must use systemd control-group termination without custom stop hooks")
	}
	if len(directives["delegate"]) != 0 && !systemdDirectiveIs(directives, "delegate", "no") || len(directives["delegatesubgroup"]) != 0 {
		return errors.New("daemon unit must not own or delegate the Linux run cgroup parent")
	}
	return nil
}

func requireLinuxSystemdListExact(directives map[string][]string, key string, expected []string) error {
	values := directives[key]
	if len(values) != 1 || !equalLinuxStrings(strings.Fields(values[0]), expected) {
		return fmt.Errorf("systemd directive %s values are %q, require exactly %q", key, values, strings.Join(expected, " "))
	}
	return nil
}

func equalLinuxStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func rejectLinuxSystemdUnitLists(directives map[string][]string, keys ...string) error {
	for _, key := range keys {
		if len(directives[key]) != 0 {
			return fmt.Errorf("unexpected %s directive %q", key, directives[key])
		}
	}
	return nil
}

func systemdListHasItem(values []string, expected string) bool {
	items := make(map[string]struct{})
	for _, value := range values {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			items = make(map[string]struct{})
			continue
		}
		for _, item := range fields {
			items[item] = struct{}{}
		}
	}
	_, found := items[expected]
	return found
}

func requireLinuxSystemdExec(directives map[string][]string, key string, expected []string) error {
	if len(expected) == 0 || len(directives[key]) != 1 || strings.TrimSpace(directives[key][0]) != strings.Join(expected, " ") {
		return fmt.Errorf("systemd directive %s must contain the exact argv %q", key, strings.Join(expected, " "))
	}
	return nil
}

func systemdDirectiveIs(directives map[string][]string, key, expected string) bool {
	return len(directives[key]) == 1 && strings.EqualFold(strings.TrimSpace(directives[key][0]), expected)
}

func linuxSystemdDropInDirectories(unit string) []string {
	return []string{
		filepath.Join("/etc/systemd/system", unit+".d"),
		filepath.Join("/etc/systemd/system.control", unit+".d"),
		filepath.Join("/etc/systemd/system.attached", unit+".d"),
		filepath.Join("/run/systemd/system", unit+".d"),
		filepath.Join("/run/systemd/system.control", unit+".d"),
		filepath.Join("/run/systemd/system.attached", unit+".d"),
		filepath.Join("/run/systemd/generator.early", unit+".d"),
		filepath.Join("/run/systemd/generator", unit+".d"),
		filepath.Join("/run/systemd/generator.late", unit+".d"),
		filepath.Join("/usr/local/lib/systemd/system", unit+".d"),
		filepath.Join("/usr/lib/systemd/system", unit+".d"),
		filepath.Join("/usr/lib/systemd/system.control", unit+".d"),
	}
}

func verifyLinuxSystemdDirectory(path string, a, b ACLSubject) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("systemd directory path must be canonical and absolute")
	}
	for directory := path; ; directory = filepath.Dir(directory) {
		info, err := LstatCanonicalPath(directory)
		if err != nil {
			return fmt.Errorf("inspect systemd unit directory %s: %w", directory, err)
		}
		identity, err := FileIdentity(info)
		if err != nil {
			return err
		}
		if err := validateLinuxSystemdDirectoryIdentity(identity, info.IsDir(), info.Mode()); err != nil {
			return fmt.Errorf("systemd unit ancestor %s is unsafe: %w", directory, err)
		}
		if err := VerifyNoUntrustedACLWrite(directory, []uint32{0}, nil, true); err != nil {
			return fmt.Errorf("systemd unit ancestor %s has an untrusted ACL writer: %w", directory, err)
		}
		for _, subject := range []ACLSubject{a, b} {
			if err := VerifyEffectiveAccess(directory, subject, 1, 2); err != nil {
				return fmt.Errorf("systemd unit ancestor %s is writable or unsearchable for UID %d: %w", directory, subject.UID, err)
			}
		}
		if directory == "/" {
			return nil
		}
	}
}

func validateLinuxSystemdDirectoryIdentity(identity StatIdentity, directory bool, mode os.FileMode) error {
	if !directory || identity.UID != 0 || identity.GID != 0 || mode.Perm()&0022 != 0 {
		return errors.New("systemd unit directories and ancestors must be root:root-owned and group/other-nonwritable")
	}
	return nil
}

func verifyLinuxOptionalSystemdDirectory(path string, a, b ACLSubject) (bool, error) {
	info, err := LstatCanonicalPath(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("systemd drop-in path %s is not a directory", path)
		}
		return true, verifyLinuxSystemdDirectory(path, a, b)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect systemd drop-in directory %s: %w", path, err)
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := LstatCanonicalPath(parent)
		if err == nil {
			if !info.IsDir() {
				return false, fmt.Errorf("systemd drop-in ancestor %s is not a directory", parent)
			}
			if err := verifyLinuxSystemdDirectory(parent, a, b); err != nil {
				return false, err
			}
			return false, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect systemd drop-in ancestor %s: %w", parent, err)
		}
		if parent == "/" {
			return false, errors.New("cannot locate an existing systemd drop-in ancestor")
		}
	}
}

func readLinuxSystemdDirectives(path string, a, b ACLSubject) (map[string][]string, error) {
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return nil, err
	}
	identity, err := FileIdentity(info)
	if err != nil || !info.Mode().IsRegular() || identity.UID != 0 || identity.GID != 0 || identity.Links != 1 || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > linuxSystemdUnitMaxBytes {
		return nil, errors.New("unit file must be a root-owned, singly-linked, non-writable regular file with a bounded size")
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return nil, fmt.Errorf("unit file has an untrusted ACL writer: %w", err)
	}
	for _, subject := range []ACLSubject{a, b} {
		if err := VerifyEffectiveAccess(path, subject, 4, 2); err != nil {
			return nil, fmt.Errorf("systemd unit file %s is unreadable or writable by UID %d: %w", path, subject.UID, err)
		}
	}
	contents, err := readBoundedLinuxFile(path, linuxSystemdUnitMaxBytes)
	if err != nil {
		return nil, err
	}
	return parseLinuxSystemdServiceDirectives(contents)
}

func parseLinuxSystemdServiceDirectives(contents []byte) (map[string][]string, error) {
	result := make(map[string][]string)
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxSystemdUnitMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := stripGitConfigComment(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("systemd line %d: %w", lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("systemd line %d has an incomplete section", lineNumber)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("systemd line %d is not a directive assignment", lineNumber)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch section {
		case "unit":
			switch key {
			case "description":
			case "after", "before", "bindsto", "requires", "requisite", "wants", "partof", "conflicts", "upholds", "onfailure", "onsuccess", "requiresmountsfor":
				result[key] = append(result[key], value)
			default:
				return nil, fmt.Errorf("unreviewed systemd Unit directive %s", key)
			}
		case "service":
			switch key {
			case "nonewprivileges", "user", "group", "supplementarygroups", "delegate", "delegatesubgroup",
				"environment", "environmentfile", "passenvironment", "unsetenvironment", "execsearchpath",
				"type", "notifyaccess", "slice", "runtimedirectory", "runtimedirectorymode", "killmode", "sendsigkill",
				"timeoutstartsec", "timeoutstopsec", "restart", "restartsec", "umask", "workingdirectory",
				"execstart", "execstartpre", "execstop", "execstoppost":
				result[key] = append(result[key], value)
			default:
				return nil, fmt.Errorf("unreviewed systemd Service directive %s", key)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func requireLinuxSystemdDirective(directives map[string][]string, key, expected string) error {
	values := directives[key]
	valid := len(values) == 1 && values[0] == expected
	if len(values) == 1 && (key == "nonewprivileges" || key == "delegate") {
		valid = strings.EqualFold(values[0], expected)
	}
	if !valid {
		return fmt.Errorf("directive %s values are %q, require exactly %q", key, values, expected)
	}
	return nil
}

func requireLinuxSystemdIdentityDirective(directives map[string][]string, key, expected string) error {
	values := directives[key]
	if len(values) != 1 || values[0] != expected {
		return fmt.Errorf("directive %s values are %q, require exact A identity %q", key, values, expected)
	}
	return nil
}

func validateLinuxSystemdOverrides(directives map[string][]string, policy linuxSystemdUnitPolicy, a, b ACLSubject) error {
	for key, values := range directives {
		if len(values) == 0 {
			continue
		}
		if key == "environment" {
			if err := validateLinuxSystemdEnvironment(map[string][]string{"environment": values}, policy.home, a, b); err != nil {
				return err
			}
			continue
		}
		return fmt.Errorf("systemd drop-in override of %s is not permitted", key)
	}
	return nil
}

func validateLinuxExecSearchPaths(values []string, a, b ACLSubject) error {
	for _, searchPath := range values {
		for _, directory := range strings.Fields(searchPath) {
			if err := verifyLinuxServicePath(directory, a, b); err != nil {
				return fmt.Errorf("ExecSearchPath directory is not trusted: %w", err)
			}
		}
	}
	return nil
}

func validateLinuxSystemdEnvironment(directives map[string][]string, aHome string, a, b ACLSubject) error {
	if len(directives["environmentfile"]) != 0 || len(directives["passenvironment"]) != 0 || len(directives["unsetenvironment"]) != 0 {
		return errors.New("EnvironmentFile/PassEnvironment/UnsetEnvironment can alter unverified credentials or Git behavior")
	}
	for _, directive := range directives["environment"] {
		assignments, err := splitLinuxSystemdEnvironment(directive)
		if err != nil {
			return err
		}
		for _, assignment := range assignments {
			key, value, found := strings.Cut(assignment, "=")
			if !found {
				return errors.New("systemd Environment contains an opaque assignment")
			}
			key = strings.ToUpper(strings.Trim(key, `"'`))
			if strings.HasPrefix(key, "GIT_") || linuxCredentialEnvironmentName(key) {
				return fmt.Errorf("systemd Environment injects forbidden Git or credential override %q", key)
			}
			switch {
			case key == "HOME":
				if value != aHome {
					return errors.New("systemd HOME must remain the configured A home; B home is only for the native launcher")
				}
			case key == "PATH":
				if err := verifyLinuxServicePath(value, a, b); err != nil {
					return err
				}
			case key == "LANG" || key == "LC_ALL" || key == "LC_CTYPE":
			case strings.HasPrefix(key, "LC_"):
			default:
				return fmt.Errorf("unapproved systemd Environment directive %q", key)
			}
		}
	}
	return validateLinuxExecSearchPaths(directives["execsearchpath"], a, b)
}

func splitLinuxSystemdEnvironment(value string) ([]string, error) {
	var result []string
	var token strings.Builder
	quoted := false
	escaped := false
	started := false
	for index := range len(value) {
		character := value[index]
		if escaped {
			if character != ' ' && character != '\t' && character != '\\' && character != '"' {
				return nil, errors.New("systemd Environment uses an unsupported escape")
			}
			token.WriteByte(character)
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			started = true
			continue
		}
		if !quoted && (character == ' ' || character == '\t') {
			if started {
				result = append(result, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteByte(character)
		started = true
	}
	if quoted || escaped {
		return nil, errors.New("systemd Environment has an unterminated quote or escape")
	}
	if started {
		result = append(result, token.String())
	}
	return result, nil
}
