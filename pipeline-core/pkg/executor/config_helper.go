package executor

import (
	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// configToSourceV2 map[string]any를 config.SourceV2로 변환
func configToSourceV2(cfg map[string]any) config.SourceV2 {
	result := config.SourceV2{}

	// Type
	if v, ok := cfg["type"].(string); ok {
		result.Type = v
	}

	// File
	if v, ok := cfg["path"].(string); ok {
		result.Path = v
	}
	if v, ok := cfg["paths"].([]any); ok {
		for _, p := range v {
			if ps, ok := p.(string); ok {
				result.Paths = append(result.Paths, ps)
			}
		}
	}
	if v, ok := cfg["format"].(string); ok {
		result.Format = v
	}

	// SQL
	if v, ok := cfg["driver"].(string); ok {
		result.Driver = v
	}
	if v, ok := cfg["dsn"].(string); ok {
		result.DSN = v
	}
	if v, ok := cfg["query"].(string); ok {
		result.Query = v
	}
	if v, ok := cfg["params"].([]any); ok {
		for _, p := range v {
			if ps, ok := p.(string); ok {
				result.Params = append(result.Params, ps)
			}
		}
	}

	// HTTP
	if v, ok := cfg["url"].(string); ok {
		result.URL = v
	}
	if v, ok := cfg["method"].(string); ok {
		result.Method = v
	}
	if v, ok := cfg["headers"].(map[string]any); ok {
		result.Headers = make(map[string]string)
		for k, val := range v {
			if vs, ok := val.(string); ok {
				result.Headers[k] = vs
			}
		}
	}
	if v, ok := cfg["body"].(string); ok {
		result.Body = v
	}

	// Auth
	if authCfg, ok := cfg["auth"].(map[string]any); ok {
		result.Auth = &config.AuthConfig{}
		if v, ok := authCfg["type"].(string); ok {
			result.Auth.Type = v
		}
		if v, ok := authCfg["username"].(string); ok {
			result.Auth.Username = v
		}
		if v, ok := authCfg["password"].(string); ok {
			result.Auth.Password = v
		}
		if v, ok := authCfg["token"].(string); ok {
			result.Auth.Token = v
		}
		if v, ok := authCfg["client_id"].(string); ok {
			result.Auth.ClientID = v
		}
		if v, ok := authCfg["client_secret"].(string); ok {
			result.Auth.ClientSecret = v
		}
		if v, ok := authCfg["token_url"].(string); ok {
			result.Auth.TokenURL = v
		}
	}

	// Kafka
	if v, ok := cfg["brokers"].([]any); ok {
		for _, b := range v {
			if bs, ok := b.(string); ok {
				result.Brokers = append(result.Brokers, bs)
			}
		}
	}
	if v, ok := cfg["topics"].([]any); ok {
		for _, t := range v {
			if ts, ok := t.(string); ok {
				result.Topics = append(result.Topics, ts)
			}
		}
	}
	if v, ok := cfg["group_id"].(string); ok {
		result.GroupID = v
	}
	if v, ok := cfg["start_offset"].(string); ok {
		result.StartOffset = v
	}
	if v, ok := cfg["min_bytes"].(float64); ok {
		result.MinBytes = int(v)
	}
	if v, ok := cfg["max_bytes"].(float64); ok {
		result.MaxBytes = int(v)
	}
	if v, ok := cfg["max_wait"].(float64); ok {
		result.MaxWait = int(v)
	}
	if v, ok := cfg["commit_interval"].(float64); ok {
		result.CommitInterval = int(v)
	}

	// SQL Event Table
	if v, ok := cfg["table"].(string); ok {
		result.Table = v
	}
	if v, ok := cfg["id_column"].(string); ok {
		result.IDColumn = v
	}
	if v, ok := cfg["timestamp_column"].(string); ok {
		result.TimestampColumn = v
	}
	if v, ok := cfg["columns"].([]any); ok {
		for _, c := range v {
			if cs, ok := c.(string); ok {
				result.Columns = append(result.Columns, cs)
			}
		}
	}
	if v, ok := cfg["where"].(string); ok {
		result.Where = v
	}
	if v, ok := cfg["order_by"].(string); ok {
		result.OrderBy = v
	}
	if v, ok := cfg["batch_size"].(float64); ok {
		result.BatchSize = int(v)
	}
	if v, ok := cfg["poll_interval"].(float64); ok {
		result.PollInterval = int(v)
	}

	// CDC
	if v, ok := cfg["host"].(string); ok {
		result.Host = v
	}
	if v, ok := cfg["port"].(float64); ok {
		result.Port = int(v)
	}
	if v, ok := cfg["username"].(string); ok {
		result.Username = v
	}
	if v, ok := cfg["password"].(string); ok {
		result.Password = v
	}
	if v, ok := cfg["database"].(string); ok {
		result.Database = v
	}
	if v, ok := cfg["tables"].([]any); ok {
		for _, t := range v {
			if ts, ok := t.(string); ok {
				result.Tables = append(result.Tables, ts)
			}
		}
	}
	if v, ok := cfg["server_id"].(float64); ok {
		result.ServerID = uint32(v)
	}
	if v, ok := cfg["slot_name"].(string); ok {
		result.SlotName = v
	}

	return result
}
