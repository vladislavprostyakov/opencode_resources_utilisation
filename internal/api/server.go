package api

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"

	"opencode-resources/internal/k8s"
)

//go:embed web
var webFS embed.FS

// Server — HTTP-сервис.
type Server struct {
	client *k8s.Client
}

// NewServer создаёт сервер.
func NewServer(c *k8s.Client) *Server {
	return &Server{client: c}
}

// Routes возвращает http.Handler со всеми маршрутами.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users", s.handleUsers)
	mux.HandleFunc("GET /api/users/all", s.handleAllUsers)
	mux.HandleFunc("GET /api/user/{user}", s.handleUser)
	mux.HandleFunc("GET /api/user/{user}/pods/{pod}/metrics", s.handlePodMetrics)
	mux.HandleFunc("GET /api/user/{user}/pods/{pod}/logs", s.handlePodLogs)
	mux.HandleFunc("GET /api/cluster", s.handleCluster)
	mux.HandleFunc("GET /api/metrics-source", s.handleMetricsSource)

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	return withCORS(mux)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.client.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"users": users})
}

func (s *Server) handleAllUsers(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetAllUsersInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	user := r.PathValue("user")
	info, err := s.client.GetUserInfo(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, info)
}

func (s *Server) handlePodMetrics(w http.ResponseWriter, r *http.Request) {
	pod := r.PathValue("pod")
	m, err := s.client.GetPodMetrics(pod)
	if m == nil {
		msg := "metrics unavailable"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, map[string]any{"available": false, "error": msg})
		return
	}
	writeJSON(w, m)
}

func (s *Server) handlePodLogs(w http.ResponseWriter, r *http.Request) {
	pod := r.PathValue("pod")
	tail := int64(200)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			tail = n
		}
	}
	all := r.URL.Query().Get("all") == "true"
	logs, err := s.client.GetPodLogs(pod, tail, all)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"logs": logs})
}

func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	info, err := s.client.GetClusterInfo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleMetricsSource(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "true" {
		s.client.RedetectSources()
	}
	writeJSON(w, s.client.GetMetricsSourceInfo())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
