package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"semantix/harness/config"
)

type setupProviderPreset struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Models      []string `json:"models"`
	Default     string   `json:"defaultModel"`
}

type setupConfiguredProvider struct {
	Name          string   `json:"name"`
	Models        []string `json:"models"`
	Default       string   `json:"defaultModel"`
	KeyConfigured bool     `json:"keyConfigured"`
}

func (s *Server) setupProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.providerSetupSnapshot(); !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := config.LoadForRootReadOnly(".")
	if err != nil {
		http.Error(w, "无法读取 Provider 配置", http.StatusInternalServerError)
		return
	}
	presets := make([]setupProviderPreset, 0)
	for _, preset := range config.CuratedProviderPresets() {
		models := make([]string, 0)
		defaultModel := ""
		for _, entry := range preset.Entries {
			models = append(models, entry.ChatModelList()...)
			if defaultModel == "" {
				defaultModel = entry.DefaultModel()
			}
		}
		presets = append(presets, setupProviderPreset{ID: preset.ID, Label: preset.Label, Description: preset.Description, Models: models, Default: defaultModel})
	}
	resolver := config.NewCredentialResolverForRoot(".")
	configured := make([]setupConfiguredProvider, 0, len(cfg.Providers))
	for i := range cfg.Providers {
		entry := &cfg.Providers[i]
		configured = append(configured, setupConfiguredProvider{
			Name: entry.Name, Models: entry.ChatModelList(), Default: entry.DefaultModel(),
			KeyConfigured: !entry.RequiresAPIKey() || resolver.ResolveGlobalFirst(entry.APIKeyEnv).Set,
		})
	}
	writeJSON(w, map[string]any{
		"presets": presets, "providers": configured, "defaultModel": cfg.DefaultModel,
		"activeModel": currentModelRef(s.ctl()),
	})
}

type setupProviderRequest struct {
	PresetID     string `json:"presetId"`
	DefaultModel string `json:"defaultModel"`
	APIKey       string `json:"apiKey"`
}

func decodeSetupProviderRequest(w http.ResponseWriter, r *http.Request) (setupProviderRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, providerSetupMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body setupProviderRequest
	if err := dec.Decode(&body); err != nil || ensureProviderSetupJSONEOF(dec) != nil {
		http.Error(w, "无效的 Provider 配置请求", http.StatusBadRequest)
		return setupProviderRequest{}, false
	}
	body.PresetID = strings.TrimSpace(body.PresetID)
	body.DefaultModel = strings.TrimSpace(body.DefaultModel)
	body.APIKey = strings.TrimSpace(body.APIKey)
	if len(body.APIKey) > 16<<10 {
		http.Error(w, "API Key 过长", http.StatusBadRequest)
		return setupProviderRequest{}, false
	}
	return body, true
}

func presetSelection(presetID, requestedModel string) (config.ProviderPreset, config.ProviderEntry, string, error) {
	preset, ok := config.CuratedProviderPreset(presetID)
	if !ok || len(preset.Entries) == 0 {
		return config.ProviderPreset{}, config.ProviderEntry{}, "", errors.New("未知 Provider 预设")
	}
	for _, entry := range preset.Entries {
		model := requestedModel
		if prefix := entry.Name + "/"; strings.HasPrefix(model, prefix) {
			model = strings.TrimPrefix(model, prefix)
		}
		if model == "" {
			model = entry.DefaultModel()
		}
		if entry.HasModel(model) {
			return preset, entry, entry.Name + "/" + model, nil
		}
	}
	return config.ProviderPreset{}, config.ProviderEntry{}, "", errors.New("所选模型不属于该 Provider")
}

func (s *Server) saveSetupProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.providerSetupSnapshot(); !ok {
		http.NotFound(w, r)
		return
	}
	body, ok := decodeSetupProviderRequest(w, r)
	if !ok {
		return
	}
	preset, selected, ref, err := presetSelection(body.PresetID, body.DefaultModel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if selected.RequiresAPIKey() && body.APIKey == "" && !config.CredentialStored(selected.APIKeyEnv) {
		http.Error(w, "API Key 不能为空", http.StatusBadRequest)
		return
	}
	err = config.EditUserConfigWithCredentials(func(cfg *config.Config) ([]config.CredentialChange, error) {
		for _, entry := range preset.Entries {
			if err := cfg.UpsertProvider(entry); err != nil {
				return nil, err
			}
			if cfg.Desktop.ProviderAccess != nil && !containsString(cfg.Desktop.ProviderAccess, entry.Name) {
				cfg.Desktop.ProviderAccess = append(cfg.Desktop.ProviderAccess, entry.Name)
			}
		}
		if err := cfg.SetDefaultModel(ref); err != nil {
			return nil, err
		}
		if body.APIKey == "" {
			return nil, nil
		}
		return []config.CredentialChange{{Key: selected.APIKeyEnv, Value: body.APIKey}}, nil
	})
	if err != nil {
		http.Error(w, "保存 Provider 配置失败", http.StatusInternalServerError)
		return
	}
	if err := s.switchModel(r.Context(), ref); err != nil {
		http.Error(w, "配置已保存，但 Provider 暂时无法激活", http.StatusConflict)
		return
	}
	s.refreshProviderSetup(ref)
	w.WriteHeader(http.StatusNoContent)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (s *Server) testSetupProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.providerSetupSnapshot(); !ok {
		http.NotFound(w, r)
		return
	}
	body, ok := decodeSetupProviderRequest(w, r)
	if !ok {
		return
	}
	_, _, ref, err := presetSelection(body.PresetID, body.DefaultModel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := config.LoadForRootReadOnly(".")
	if err != nil {
		http.Error(w, "无法读取 Provider 配置", http.StatusInternalServerError)
		return
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok || (entry.RequiresAPIKey() && entry.APIKey() == "") {
		http.Error(w, "请先保存 API Key", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, err := entry.FetchModels(ctx); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "message": "连接失败，请检查 API Key 和网络后重试"})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": "连接成功"})
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
