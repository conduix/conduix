package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/conduix/conduix/control-plane/pkg/models"
	"github.com/conduix/conduix/shared/types"
)

const unregisteredImportSource = `package s

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/go-resty/resty/v2/middleware"
	sdk "github.com/conduix/conduix/plugin-sdk"
)

type Stage struct{}
var _ sdk.NativeStage = (*Stage)(nil)
func (s *Stage) Init(map[string]any) error { return nil }
func (s *Stage) Process(r map[string]any) (map[string]any, error) { fmt.Sprint(uuid.NewString()); return r, nil }
func (s *Stage) Close() error { return nil }
`

// 미등록 import 로 저장하면 400 + BUSINESS_MISSING_MODULES 와 함께 Details 에
// {import 경로: 제안 모듈 경로} 가 실려야 UI 가 "추가하고 저장" 을 만들 수 있다.
// 등록된 모듈(uuid)은 목록에 없어야 한다.
func TestCreatePlugin_MissingModulesAreStructured(t *testing.T) {
	h, db := newPluginTestHandler(t)
	require.NoError(t, db.AutoMigrate(&models.AllowedModule{}))
	require.NoError(t, db.Create(&models.AllowedModule{ModulePath: "github.com/google/uuid", Version: "v1.6.0", Status: "active"}).Error)
	// moduleResolver 가 nil 이라 GOPROXY 없이 휴리스틱(github 3세그먼트 + v2 접미 포함 4세그먼트가 아님)으로 제안된다.

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/plugins", h.CreatePlugin)

	body := map[string]any{"name": "resty-stage", "type": "native", "source_code": unregisteredImportSource}
	raw, _ := json.Marshal(body)
	w := doJSON(r, http.MethodPost, "/plugins", string(raw))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var resp types.APIResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, types.ErrCodeMissingModules, resp.Error.Code)
	require.Equal(t, map[string]string{
		"github.com/go-resty/resty/v2/middleware": "github.com/go-resty/resty",
	}, resp.Error.Details, "등록된 uuid 는 빠지고, 미등록 import 만 제안 모듈 경로와 함께 온다")

	var count int64
	require.NoError(t, db.Model(&models.Plugin{}).Count(&count).Error)
	require.Zero(t, count, "거부된 저장은 plugin 을 만들지 않는다")
}
