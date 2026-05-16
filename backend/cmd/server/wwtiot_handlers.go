package main

import (
	"encoding/json"
	"net/http"

	"github.com/qiyue2015/device-platform/internal/cloudapi/wwtiot"
)

func registerWWTIOTRoutes(mux *http.ServeMux, client *wwtiot.Client) {
	mux.HandleFunc("/v1/cloud-api/wwtiot/command-preview", handleWWTIOTCommandPreview(client))
	mux.HandleFunc("/v1/provider-callbacks/wwtiot", handleWWTIOTCallback)
}

func handleWWTIOTCommandPreview(client *wwtiot.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCloudJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
				"error": "method_not_allowed",
			})
			return
		}
		var req struct {
			CommandType      string `json:"command_type"`
			ProviderDeviceID string `json:"provider_device_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeCloudJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": "invalid_json",
			})
			return
		}
		built, err := client.BuildCommand(req.CommandType, req.ProviderDeviceID)
		if err != nil {
			writeCloudJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		ack, err := client.SendCommand(built)
		if err != nil && !ack.Acked {
			writeCloudJSON(w, http.StatusBadGateway, map[string]interface{}{
				"request": built,
				"ack":     ack,
				"error":   err.Error(),
			})
			return
		}
		writeCloudJSON(w, http.StatusAccepted, map[string]interface{}{
			"request": built,
			"ack":     ack,
		})
	}
}

func handleWWTIOTCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeCloudJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "method_not_allowed",
		})
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeCloudJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "invalid_json",
		})
		return
	}
	callback := wwtiot.NormalizeCallback(payload)
	raw := wwtiot.NewRawMessage("uplink", payload, map[string]interface{}{
		"provider_device_id": callback.ProviderDeviceID,
		"command_type":       callback.CommandType,
		"vendor_command":     callback.VendorCommand,
		"lock_status":        callback.LockStatus,
		"battery_percent":    callback.BatteryPercent,
	}, "success")
	writeCloudJSON(w, http.StatusAccepted, map[string]interface{}{
		"accepted":             true,
		"provider":             wwtiot.Provider,
		"access_type":          wwtiot.AccessType,
		"adapter":              wwtiot.Adapter,
		"normalized":           callback,
		"raw_message":          raw,
		"real_callback_needed": callback.ProviderDeviceID == "",
	})
}

func writeCloudJSON(w http.ResponseWriter, status int, body map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
