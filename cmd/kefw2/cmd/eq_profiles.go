// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const eqProfileDocumentVersion = 1

type eqProfileOrigin struct {
	Name  string `json:"name,omitempty"`
	Model string `json:"model,omitempty"`
}

type savedEQProfile struct {
	Version     int             `json:"version"`
	Name        string          `json:"name"`
	CapturedAt  time.Time       `json:"captured_at"`
	Speaker     eqProfileOrigin `json:"speaker,omitempty"`
	Source      string          `json:"source,omitempty"`
	ProfileName string          `json:"profile_name"`
	Profile     json.RawMessage `json:"profile"`
}

type savedEQProfileSummary struct {
	Name        string          `json:"name"`
	ProfileName string          `json:"profile_name"`
	CapturedAt  time.Time       `json:"captured_at"`
	Speaker     eqProfileOrigin `json:"speaker,omitempty"`
	Source      string          `json:"source,omitempty"`
}

func eqProfileDirectory() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating EQ profile directory: %w", err)
	}
	return filepath.Join(dir, "kefw2", "eq_profiles"), nil
}

func eqProfileFilePath(name string) (string, error) {
	if name == "" || strings.TrimSpace(name) != name || name == "." || name == ".." ||
		strings.HasPrefix(name, "-") || strings.ContainsAny(name, `/\:`) ||
		strings.HasSuffix(name, ".") || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
		utf8.RuneCountInString(name) > 128 {
		return "", fmt.Errorf("invalid EQ profile name %q", name)
	}
	dir, err := eqProfileDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

func decodeEQProfileDocument(data []byte, name string, allowRename bool) (savedEQProfile, error) {
	var doc savedEQProfile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return doc, fmt.Errorf("parsing saved EQ profile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return doc, fmt.Errorf("parsing saved EQ profile: multiple JSON values")
		}
		return doc, fmt.Errorf("parsing saved EQ profile: %w", err)
	}
	if doc.Version != eqProfileDocumentVersion {
		return doc, fmt.Errorf("unsupported EQ profile document version %d", doc.Version)
	}
	if _, err := eqProfileFilePath(doc.Name); err != nil {
		return doc, err
	}
	if !allowRename && doc.Name != name {
		return doc, fmt.Errorf("saved EQ profile name does not match file name")
	}
	profile, _, err := decodeEQProfilePayload(doc.Profile)
	if err != nil {
		return doc, fmt.Errorf("invalid saved EQ profile: %w", err)
	}
	if doc.ProfileName != profile.ProfileName {
		return doc, fmt.Errorf("saved EQ profile name does not match profile payload")
	}
	if doc.CapturedAt.IsZero() {
		return doc, fmt.Errorf("saved EQ profile has no capture timestamp")
	}
	if allowRename {
		doc.Name = name
	}
	return doc, nil
}

func decodeEQProfileImport(data []byte, name string) (savedEQProfile, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return savedEQProfile{}, fmt.Errorf("parsing EQ profile import: expected a JSON object")
	}
	if _, isDocument := fields["version"]; isDocument {
		doc, err := decodeEQProfileDocument(data, name, true)
		if err != nil {
			return savedEQProfile{}, err
		}
		return doc, nil
	}
	profile, raw, err := decodeEQProfilePayload(data)
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("parsing EQ profile import: %w", err)
	}
	return savedEQProfile{
		Version: eqProfileDocumentVersion, Name: name, CapturedAt: time.Now().UTC(),
		ProfileName: profile.ProfileName, Profile: raw,
	}, nil
}

func readSavedEQProfile(name string) (savedEQProfile, error) {
	path, err := eqProfileFilePath(name)
	if err != nil {
		return savedEQProfile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("locating saved EQ profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return savedEQProfile{}, fmt.Errorf("saved EQ profile is not a regular file")
	}
	data, err := os.ReadFile(path) //nolint:gosec // Name is validated under the application config directory.
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("reading saved EQ profile: %w", err)
	}
	doc, err := decodeEQProfileDocument(data, name, false)
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("saved EQ profile %q: %w", name, err)
	}
	return doc, nil
}

func listSavedEQProfiles() ([]savedEQProfileSummary, error) {
	dir, err := eqProfileDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []savedEQProfileSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing saved EQ profiles: %w", err)
	}
	profiles := make([]savedEQProfileSummary, 0)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("saved EQ profile %q is a symbolic link", entry.Name())
		}
		if !entry.Type().IsRegular() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		doc, err := readSavedEQProfile(name)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, savedEQProfileSummary{
			Name: doc.Name, ProfileName: doc.ProfileName, CapturedAt: doc.CapturedAt,
			Speaker: doc.Speaker, Source: doc.Source,
		})
	}
	slices.SortFunc(profiles, func(a, b savedEQProfileSummary) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return profiles, nil
}

func atomicWriteEQProfile(path string, data []byte, overwrite bool, createDir bool) error {
	dir := filepath.Dir(path)
	if createDir {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating EQ profile directory: %w", err)
		}
	}
	temp, err := os.CreateTemp(dir, ".eq-profile-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary EQ profile: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	writeErr := error(nil)
	if err := temp.Chmod(0600); err != nil {
		writeErr = err
	} else if n, err := temp.Write(data); err != nil {
		writeErr = err
	} else if n != len(data) {
		writeErr = io.ErrShortWrite
	} else {
		writeErr = temp.Sync()
	}
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("writing temporary EQ profile: %w", err)
	}
	if overwrite {
		if err := os.Rename(tempPath, path); err != nil {
			return fmt.Errorf("replacing EQ profile: %w", err)
		}
		return nil
	}
	if err := os.Link(tempPath, path); err != nil {
		return fmt.Errorf("creating EQ profile without overwrite: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("removing temporary EQ profile: %w", err)
	}
	return nil
}

func writeSavedEQProfile(doc savedEQProfile, overwrite bool) error {
	path, err := eqProfileFilePath(doc.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding saved EQ profile: %w", err)
	}
	return atomicWriteEQProfile(path, append(data, '\n'), overwrite, true)
}

func saveCurrentEQProfile(cmd *cobra.Command, name string, overwrite bool) (savedEQProfile, error) {
	if _, err := eqProfileFilePath(name); err != nil {
		return savedEQProfile{}, err
	}
	source, err := currentSpeaker.Source(cmd.Context())
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("getting current source: %w", err)
	}
	raw, err := currentSpeaker.GetEQProfileV2Raw(cmd.Context())
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("getting EQ profile: %w", err)
	}
	profile, raw, err := decodeEQProfilePayload(raw)
	if err != nil {
		return savedEQProfile{}, err
	}
	doc := savedEQProfile{
		Version: eqProfileDocumentVersion, Name: name, CapturedAt: time.Now().UTC(),
		Speaker: eqProfileOrigin{Name: currentSpeaker.Name, Model: currentSpeaker.Model},
		Source:  string(source), ProfileName: profile.ProfileName, Profile: raw,
	}
	if err := writeSavedEQProfile(doc, overwrite); err != nil {
		return savedEQProfile{}, err
	}
	return doc, nil
}

func importEQProfile(name, file string, overwrite bool) (savedEQProfile, error) {
	if _, err := eqProfileFilePath(name); err != nil {
		return savedEQProfile{}, err
	}
	data, err := os.ReadFile(file) //nolint:gosec // The user explicitly supplies the import file.
	if err != nil {
		return savedEQProfile{}, fmt.Errorf("reading EQ profile import: %w", err)
	}
	doc, err := decodeEQProfileImport(data, name)
	if err != nil {
		return savedEQProfile{}, err
	}
	if err := writeSavedEQProfile(doc, overwrite); err != nil {
		return savedEQProfile{}, err
	}
	return doc, nil
}

func deleteSavedEQProfile(name string) error {
	path, err := eqProfileFilePath(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("locating saved EQ profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("saved EQ profile is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting saved EQ profile: %w", err)
	}
	return nil
}

func eqProfileNameCompletions(prefix string) ([]string, cobra.ShellCompDirective) {
	profiles, err := listSavedEQProfiles()
	if err != nil {
		cobra.CompErrorln(err.Error())
		return nil, cobra.ShellCompDirectiveError
	}
	result := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if strings.HasPrefix(strings.ToLower(profile.Name), strings.ToLower(prefix)) {
			result = append(result, profile.Name)
		}
	}
	return result, cobra.ShellCompDirectiveNoFileComp
}

func savedEQProfileCompletion(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return eqProfileNameCompletions(prefix)
}

func importEQProfileCompletion(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return eqProfileNameCompletions(prefix)
	}
	if len(args) == 1 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func exportEQProfileCompletion(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return eqProfileNameCompletions(prefix)
	}
	if len(args) == 1 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

var eqProfileShowCmd = &cobra.Command{
	Use:               "show [NAME]",
	Short:             "Show the active or a locally saved EQ profile",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: savedEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			raw, err := currentSpeaker.GetEQProfileV2Raw(cmd.Context())
			if err != nil {
				return fmt.Errorf("getting EQ profile: %w", err)
			}
			if jsonOutput(cmd) {
				return writeSpeakerJSON(cmd, struct {
					EQProfile json.RawMessage `json:"eq_profile"`
				}{raw})
			}
			return writeEQProfileText(cmd, raw)
		}
		doc, err := readSavedEQProfile(args[0])
		if err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, doc)
		}
		return writeEQProfileText(cmd, doc.Profile)
	},
}

var eqProfileSaveCmd = &cobra.Command{
	Use:               "save NAME",
	Short:             "Save the active EQ profile to the local library",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: savedEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		overwrite, _ := cmd.Flags().GetBool("force")
		doc, err := saveCurrentEQProfile(cmd, args[0], overwrite)
		if err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return writeSpeakerJSON(cmd, struct {
				actionResult
				Name        string    `json:"name"`
				ProfileName string    `json:"profile_name"`
				CapturedAt  time.Time `json:"captured_at"`
			}{actionResult{"save_eq_profile", true}, doc.Name, doc.ProfileName, doc.CapturedAt})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Saved EQ profile %q (%s)\n", doc.Name, doc.ProfileName)
		return err
	},
}

var eqProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List profiles in the local EQ library",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		profiles, err := listSavedEQProfiles()
		if err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, struct {
				Items []savedEQProfileSummary `json:"items"`
			}{profiles})
		}
		if len(profiles) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "No saved EQ profiles found.")
			return err
		}
		for _, profile := range profiles {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n",
				profile.Name, profile.ProfileName, profile.Speaker.Model, profile.CapturedAt.Format(time.RFC3339))
			if err != nil {
				return err
			}
		}
		return nil
	},
}

var eqProfileLoadCmd = &cobra.Command{
	Use:               "load NAME",
	Short:             "Load a local EQ profile onto the active source",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: savedEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		doc, err := readSavedEQProfile(args[0])
		if err != nil {
			return err
		}
		source, err := currentSpeaker.Source(cmd.Context())
		if err != nil {
			return fmt.Errorf("getting current source: %w", err)
		}
		if err := currentSpeaker.SetEQProfileV2Raw(cmd.Context(), doc.Profile); err != nil {
			return fmt.Errorf("loading EQ profile: %w", err)
		}
		if jsonOutput(cmd) {
			return writeSpeakerJSON(cmd, struct {
				actionResult
				Name        string `json:"name"`
				ProfileName string `json:"profile_name"`
				Source      string `json:"source"`
			}{actionResult{"load_eq_profile", true}, doc.Name, doc.ProfileName, string(source)})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Loaded EQ profile %q (%s) to %s source\n",
			doc.Name, doc.ProfileName, source)
		return err
	},
}

var eqProfileDeleteCmd = &cobra.Command{
	Use:               "delete NAME",
	Short:             "Delete a profile from the local EQ library",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: savedEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := deleteSavedEQProfile(args[0]); err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, struct {
				actionResult
				Name string `json:"name"`
			}{actionResult{"delete_eq_profile", true}, args[0]})
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted EQ profile %q\n", args[0])
		return err
	},
}

var eqProfileImportCmd = &cobra.Command{
	Use:               "import NAME FILE",
	Short:             "Import a JSON EQ profile backup to the local library",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: importEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		overwrite, _ := cmd.Flags().GetBool("force")
		doc, err := importEQProfile(args[0], args[1], overwrite)
		if err != nil {
			return err
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, struct {
				actionResult
				Name        string `json:"name"`
				ProfileName string `json:"profile_name"`
			}{actionResult{"import_eq_profile", true}, doc.Name, doc.ProfileName})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Imported EQ profile %q (%s)\n", doc.Name, doc.ProfileName)
		return err
	},
}

var eqProfileExportCmd = &cobra.Command{
	Use:               "export NAME [FILE]",
	Short:             "Export a local EQ profile as JSON",
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: exportEQProfileCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		overwrite, _ := cmd.Flags().GetBool("force")
		if len(args) == 1 && overwrite {
			return fmt.Errorf("--force requires an export file")
		}
		doc, err := readSavedEQProfile(args[0])
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding EQ profile export: %w", err)
		}
		data = append(data, '\n')
		if len(args) == 2 {
			if err := atomicWriteEQProfile(args[1], data, overwrite, false); err != nil {
				return err
			}
			if jsonOutput(cmd) {
				return writeJSON(cmd, struct {
					actionResult
					Name string `json:"name"`
					Path string `json:"path"`
				}{actionResult{"export_eq_profile", true}, doc.Name, args[1]})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Exported EQ profile %q to %s\n", doc.Name, args[1])
			return err
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, json.RawMessage(bytes.TrimSpace(data)))
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	},
}

func writeEQProfileText(cmd *cobra.Command, raw json.RawMessage) error {
	var output bytes.Buffer
	if err := json.Indent(&output, raw, "", "  "); err != nil {
		return fmt.Errorf("formatting EQ profile: %w", err)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), output.String())
	return err
}

func init() {
	eqProfileSaveCmd.Flags().Bool("force", false, "Replace an existing local profile")
	eqProfileImportCmd.Flags().Bool("force", false, "Replace an existing local profile")
	eqProfileExportCmd.Flags().Bool("force", false, "Replace an existing export file")

	setJSONHandler(eqProfileShowCmd, runEQProfileShowJSON)
	setJSONHandler(eqProfileSaveCmd, runEQProfileSaveJSON)
	setJSONHandler(eqProfileListCmd, runEQProfileListJSON)
	setJSONHandler(eqProfileLoadCmd, runEQProfileLoadJSON)
	setJSONHandler(eqProfileDeleteCmd, runEQProfileDeleteJSON)
	setJSONHandler(eqProfileImportCmd, runEQProfileImportJSON)
	setJSONHandler(eqProfileExportCmd, runEQProfileExportJSON)
	eqProfileCmd.AddCommand(eqProfileShowCmd, eqProfileSaveCmd, eqProfileListCmd,
		eqProfileLoadCmd, eqProfileDeleteCmd, eqProfileImportCmd, eqProfileExportCmd)
}

func runEQProfileShowJSON(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		raw, err := currentSpeaker.GetEQProfileV2Raw(cmd.Context())
		if err != nil {
			return fmt.Errorf("getting EQ profile: %w", err)
		}
		return writeSpeakerJSON(cmd, struct {
			EQProfile json.RawMessage `json:"eq_profile"`
		}{raw})
	}
	doc, err := readSavedEQProfile(args[0])
	if err != nil {
		return err
	}
	return writeJSON(cmd, doc)
}

func runEQProfileSaveJSON(cmd *cobra.Command, args []string) error {
	overwrite, _ := cmd.Flags().GetBool("force")
	doc, err := saveCurrentEQProfile(cmd, args[0], overwrite)
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Name        string    `json:"name"`
		ProfileName string    `json:"profile_name"`
		CapturedAt  time.Time `json:"captured_at"`
	}{actionResult{"save_eq_profile", true}, doc.Name, doc.ProfileName, doc.CapturedAt})
}

func runEQProfileListJSON(cmd *cobra.Command, _ []string) error {
	profiles, err := listSavedEQProfiles()
	if err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		Items []savedEQProfileSummary `json:"items"`
	}{profiles})
}

func runEQProfileLoadJSON(cmd *cobra.Command, args []string) error {
	doc, err := readSavedEQProfile(args[0])
	if err != nil {
		return err
	}
	source, err := currentSpeaker.Source(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting current source: %w", err)
	}
	if err := currentSpeaker.SetEQProfileV2Raw(cmd.Context(), doc.Profile); err != nil {
		return fmt.Errorf("loading EQ profile: %w", err)
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Name        string `json:"name"`
		ProfileName string `json:"profile_name"`
		Source      string `json:"source"`
	}{actionResult{"load_eq_profile", true}, doc.Name, doc.ProfileName, string(source)})
}

func runEQProfileDeleteJSON(cmd *cobra.Command, args []string) error {
	if err := deleteSavedEQProfile(args[0]); err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		actionResult
		Name string `json:"name"`
	}{actionResult{"delete_eq_profile", true}, args[0]})
}

func runEQProfileImportJSON(cmd *cobra.Command, args []string) error {
	overwrite, _ := cmd.Flags().GetBool("force")
	doc, err := importEQProfile(args[0], args[1], overwrite)
	if err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		actionResult
		Name        string `json:"name"`
		ProfileName string `json:"profile_name"`
	}{actionResult{"import_eq_profile", true}, doc.Name, doc.ProfileName})
}

func runEQProfileExportJSON(cmd *cobra.Command, args []string) error {
	overwrite, _ := cmd.Flags().GetBool("force")
	if len(args) == 1 && overwrite {
		return fmt.Errorf("--force requires an export file")
	}
	doc, err := readSavedEQProfile(args[0])
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding EQ profile export: %w", err)
	}
	data = append(data, '\n')
	if len(args) == 2 {
		if err := atomicWriteEQProfile(args[1], data, overwrite, false); err != nil {
			return err
		}
		return writeJSON(cmd, struct {
			actionResult
			Name string `json:"name"`
			Path string `json:"path"`
		}{actionResult{"export_eq_profile", true}, doc.Name, args[1]})
	}
	return writeJSON(cmd, doc)
}
