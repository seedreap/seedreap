package testing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// ArrCommand represents a command received by the mock Arr server.
type ArrCommand struct {
	Name      string
	Path      string
	Timestamp time.Time
}

// Command channel buffer size for ArrServer.
const arrCommandBufferSize = 100

// ArrServer is a mock Sonarr/Radarr API server for testing.
type ArrServer struct {
	*httptest.Server

	mu       sync.RWMutex
	commands []ArrCommand
	appName  string
	version  string

	// commandCh is used to notify waiters when a new command is received
	commandCh chan ArrCommand
}

// NewArrServer creates a new mock Arr server.
// appName should be "Sonarr" or "Radarr" for realistic responses.
func NewArrServer(appName string) *ArrServer {
	s := &ArrServer{
		commands:  make([]ArrCommand, 0),
		appName:   appName,
		version:   "4.0.0.0",
		commandCh: make(chan ArrCommand, arrCommandBufferSize),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/system/status", s.handleSystemStatus)
	mux.HandleFunc("POST /api/v3/command", s.handleCommand)

	s.Server = httptest.NewServer(mux)
	return s
}

// GetCommands returns all commands received by the server.
func (s *ArrServer) GetCommands() []ArrCommand {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ArrCommand, len(s.commands))
	copy(result, s.commands)
	return result
}

// GetCommandsByName returns commands with the specified name.
func (s *ArrServer) GetCommandsByName(name string) []ArrCommand {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ArrCommand
	for _, cmd := range s.commands {
		if cmd.Name == name {
			result = append(result, cmd)
		}
	}
	return result
}

// WaitForCommand waits for a command with the specified name to be received.
// Returns the command or an error if the timeout is reached.
func (s *ArrServer) WaitForCommand(name string, timeout time.Duration) (*ArrCommand, error) {
	// First check if we already have the command
	s.mu.RLock()
	for _, cmd := range s.commands {
		if cmd.Name == name {
			s.mu.RUnlock()
			return &cmd, nil
		}
	}
	s.mu.RUnlock()

	// Wait for new commands
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case cmd := <-s.commandCh:
			if cmd.Name == name {
				return &cmd, nil
			}
		case <-timer.C:
			return nil, &WaitTimeoutError{
				What:    "command " + name,
				Timeout: timeout,
			}
		}
	}
}

// Reset clears all recorded commands.
func (s *ArrServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commands = make([]ArrCommand, 0)

	// Drain the command channel
	for {
		select {
		case <-s.commandCh:
		default:
			return
		}
	}
}

// arrSystemStatus matches the Arr API response format.
type arrSystemStatus struct {
	Version string `json:"version"`
	AppName string `json:"appName"`
}

// handleSystemStatus handles GET /api/v3/system/status.
func (s *ArrServer) handleSystemStatus(w http.ResponseWriter, _ *http.Request) {
	resp := arrSystemStatus{
		Version: s.version,
		AppName: s.appName,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// arrCommandRequest matches the Arr API request format.
type arrCommandRequest struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// arrCommandResponse matches the Arr API response format.
type arrCommandResponse struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Queued    time.Time `json:"queued"`
	Started   time.Time `json:"started"`
	StateTime time.Time `json:"stateChangeTime"`
}

// handleCommand handles POST /api/v3/command.
func (s *ArrServer) handleCommand(w http.ResponseWriter, r *http.Request) {
	var req arrCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cmd := ArrCommand{
		Name:      req.Name,
		Path:      req.Path,
		Timestamp: time.Now(),
	}

	// Record the command
	s.mu.Lock()
	s.commands = append(s.commands, cmd)
	s.mu.Unlock()

	// Notify waiters (non-blocking)
	select {
	case s.commandCh <- cmd:
	default:
		// Channel full, command is still recorded
	}

	// Return success response
	resp := arrCommandResponse{
		ID:        len(s.commands),
		Name:      req.Name,
		Status:    "queued",
		Queued:    cmd.Timestamp,
		Started:   cmd.Timestamp,
		StateTime: cmd.Timestamp,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// WaitTimeoutError is returned when a wait operation times out.
type WaitTimeoutError struct {
	What    string
	Timeout time.Duration
}

func (e *WaitTimeoutError) Error() string {
	return "timeout waiting for " + e.What + " after " + e.Timeout.String()
}
