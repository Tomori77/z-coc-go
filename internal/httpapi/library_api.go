package httpapi

import (
	"net/http"
	"strings"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/store"
	"github.com/Tomori77/z-coc/internal/validate"
)

var libraryTypes = map[string]bool{
	"original":         true,
	"reprint":          true,
	"ai_adapt":         true,
	"ai_adapt_private": true,
}

func (s *Server) handleLibraryAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("action") {
	case "list":
		s.libraryList(w, r)
	case "detail":
		s.libraryDetail(w, r)
	case "create":
		s.libraryCreate(w, r)
	case "delete":
		s.libraryDelete(w, r)
	case "increment_downloads":
		s.libraryIncrementDownloads(w, r)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

func (s *Server) libraryList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "library_list", 240, 300) {
		return
	}

	section := "reading"
	if r.URL.Query().Get("section") == "restricted" {
		section = "restricted"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 80 {
		query = query[:80]
	}

	var (
		modules []store.ModuleRow
		err     error
	)
	if section == "restricted" {
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userID != "" {
			normalized, ok := validate.UserID(userID)
			if !ok {
				errorJSON(w, http.StatusBadRequest, "Invalid user id")
				return
			}
			userID = normalized
		}
		ownerHash := strings.Repeat("0", 64)
		if hash, ok := validate.OwnerTokenMatches(auth.BearerToken(r)); ok {
			ownerHash = hash
		}
		modules, err = s.store.ListRestrictedModules(r.Context(), userID, ownerHash, query, s.cfg.ListLimit)
	} else {
		modules, err = s.store.ListReadingModules(r.Context(), query, s.cfg.ListLimit)
	}
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Library query failed")
		return
	}

	out := make([]map[string]any, 0, len(modules))
	for _, module := range modules {
		out = append(out, moduleToMap(module, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "modules": out})
}

func (s *Server) libraryDetail(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "library_detail", 300, 300) {
		return
	}
	id := phpAtoi(r.URL.Query().Get("id"))
	if id < 1 {
		errorJSON(w, http.StatusBadRequest, "Invalid module id")
		return
	}
	module, err := s.store.GetModule(r.Context(), id)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Library query failed")
		return
	}
	if module == nil {
		errorJSON(w, http.StatusNotFound, "Module not found")
		return
	}

	isPublic := module.Type == "original" || module.Type == "reprint" || module.Type == "ai_adapt"
	if !isPublic {
		userID, ok := validate.UserID(r.URL.Query().Get("user_id"))
		if !ok {
			errorJSON(w, http.StatusBadRequest, "Invalid user id")
			return
		}
		ownerHash, ok := validate.OwnerTokenMatches(auth.BearerToken(r))
		if !ok || module.AuthorID != userID || module.OwnerTokenHash != ownerHash {
			errorJSON(w, http.StatusForbidden, "Module access denied")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "module": moduleToMap(*module, true)})
}

func (s *Server) libraryCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "library_create", 60, 300) {
		return
	}
	data, ok := readJSONBody(w, r, s.cfg.MaxContentBytes+32768)
	if !ok {
		return
	}

	title, ok := libraryText(w, asString(data["title"]), 255, "Title", true)
	if !ok {
		return
	}
	description, ok := libraryText(w, asString(data["description"]), 4000, "Description", false)
	if !ok {
		return
	}
	content, ok := libraryText(w, asString(data["content"]), int(s.cfg.MaxContentBytes), "Content", true)
	if !ok {
		return
	}
	authorNameRaw := "游客"
	if raw, present := data["author_name"]; present && raw != nil {
		authorNameRaw = asString(raw)
	}
	authorName, ok := libraryText(w, authorNameRaw, 120, "Author name", false)
	if !ok {
		return
	}

	authorID, ok := validate.UserID(asString(data["author_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	ownerHash, ok := validate.OwnerTokenHash(asString(data["owner_token"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid owner token")
		return
	}

	moduleType := asString(data["type"])
	if moduleType == "" {
		moduleType = "original"
	}
	if !libraryTypes[moduleType] {
		errorJSON(w, http.StatusBadRequest, "Invalid module type")
		return
	}

	minPlayers, ok := libraryNullableInt(w, data["min_players"])
	if !ok {
		return
	}
	maxPlayers, ok := libraryNullableInt(w, data["max_players"])
	if !ok {
		return
	}
	if minPlayers != nil && maxPlayers != nil && *minPlayers > *maxPlayers {
		errorJSON(w, http.StatusBadRequest, "Minimum players cannot exceed maximum players")
		return
	}

	id, err := s.store.CreateModule(r.Context(), title, description, content, authorName, authorID, ownerHash, moduleType, minPlayers, maxPlayers)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Library insert failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) libraryDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "library_delete", 60, 300) {
		return
	}
	data, ok := readJSONBody(w, r, 64*1024)
	if !ok {
		return
	}

	id := phpAtoi(asString(data["id"]))
	if id < 1 {
		errorJSON(w, http.StatusBadRequest, "Invalid module id")
		return
	}
	authorID, ok := validate.UserID(asString(data["author_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	ownerHash, ok := validate.OwnerTokenHash(asString(data["owner_token"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid owner token")
		return
	}

	storedAuthor, storedOwner, found, err := s.store.ModuleOwner(r.Context(), id)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Library delete failed")
		return
	}
	if !found {
		errorJSON(w, http.StatusNotFound, "Module not found")
		return
	}
	if storedAuthor != authorID || storedOwner != ownerHash {
		errorJSON(w, http.StatusForbidden, "You can only delete modules from this browser")
		return
	}
	if err := s.store.DeleteModule(r.Context(), id); err != nil {
		errorJSON(w, http.StatusInternalServerError, "Library delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) libraryIncrementDownloads(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "library_download", 300, 300) {
		return
	}
	data, ok := readJSONBody(w, r, 64*1024)
	if !ok {
		return
	}
	id := phpAtoi(asString(data["id"]))
	if id < 1 {
		errorJSON(w, http.StatusBadRequest, "Invalid module id")
		return
	}
	_ = s.store.IncrementDownloads(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// libraryText trims and validates a text field, matching library_text().
func libraryText(w http.ResponseWriter, value string, maxBytes int, field string, required bool) (string, bool) {
	value = strings.TrimSpace(value)
	if required && value == "" {
		errorJSON(w, http.StatusBadRequest, field+" is required")
		return "", false
	}
	if len(value) > maxBytes {
		errorJSON(w, http.StatusRequestEntityTooLarge, field+" is too long")
		return "", false
	}
	return value, true
}

// libraryNullableInt validates an optional 1..20 player count.
func libraryNullableInt(w http.ResponseWriter, value any) (*int64, bool) {
	if value == nil {
		return nil, true
	}
	if s, ok := value.(string); ok && s == "" {
		return nil, true
	}
	n := phpIntCast(value)
	if n < 1 || n > 20 {
		errorJSON(w, http.StatusBadRequest, "Invalid player count")
		return nil, false
	}
	return &n, true
}

// moduleToMap renders a module row like library_module_row().
func moduleToMap(module store.ModuleRow, includeContent bool) map[string]any {
	out := map[string]any{
		"id":             module.ID,
		"created_at":     module.CreatedAt,
		"title":          module.Title,
		"description":    module.Description,
		"author_name":    module.AuthorName,
		"author_id":      module.AuthorID,
		"type":           module.Type,
		"downloads":      module.Downloads,
		"min_players":    module.MinPlayers,
		"max_players":    module.MaxPlayers,
		"content_length": module.ContentLength,
	}
	if includeContent {
		content := ""
		if module.Content != nil {
			content = *module.Content
		}
		out["content"] = content
	}
	return out
}
