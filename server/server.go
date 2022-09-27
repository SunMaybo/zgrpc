package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/SunMaybo/zero/common/zgin"
	"github.com/SunMaybo/zero/common/zlog"
	"github.com/SunMaybo/zgrpc/dto"
	"github.com/SunMaybo/zgrpc/grpcurl"
	"github.com/SunMaybo/zgrpc/static"
	"github.com/SunMaybo/zgrpc/svc"
	"github.com/boltdb/bolt"
	grpcurl2 "github.com/fullstorydev/grpcurl"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/pkg/browser"
	"github.com/satori/go.uuid"
	"net/http"
	"os"
	"os/user"
	"sort"
	"strings"
	"sync"
	"time"
)

type Server struct {
	svcCtx *svc.ServiceContext
	server *zgin.Server
}

func NewServer(svc *svc.ServiceContext) *Server {
	if svc.Cfg.Zero.Server.Timeout <= 0 {
		svc.Cfg.Zero.Server.Timeout = 30
	}
	if svc.Cfg.Zero.Server.Port <= 0 {
		svc.Cfg.Zero.Server.Port = 3000
	}
	return &Server{
		svcCtx: svc,
		server: zgin.NewServerWithTimeout(svc.Cfg.Zero.Server.Port, time.Duration(svc.Cfg.Zero.Server.Timeout)*time.Second),
	}
}

func (s *Server) Start() {
	for _, method := range s.svcCtx.GrpcSession.Methods {
		saveMetrics(s.svcCtx.TaskDB, s.svcCtx.Cfg.Feature, method.GetService().GetFullyQualifiedName(), method.GetName(), 0)
	}
	if err := browser.OpenURL("http://127.0.0.1:" + fmt.Sprintf("%d", s.svcCtx.Cfg.Zero.Server.Port)); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open browser: %v\n", err)
	}
	s.server.Start(func(engine *gin.Engine) {
		engine.GET("/assets/*filepath", gin.WrapH(http.FileServer(http.FS(static.Static))))
		engine.GET("/", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, "/assets/index.html")
		})
		engine.GET("/api/system", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			ginCtx.JSON(200, gin.H{
				"target": s.svcCtx.Cfg.Target,
			})
		}))
		engine.GET("/api/service", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			var services []dto.ServiceResponse
			for _, service := range s.svcCtx.GrpcSession.Services {
				services = append(services, dto.ServiceResponse{
					ServiceName: service,
				})
			}
			ginCtx.JSON(200, services)
		}))
		engine.GET("/api/method/:service_name", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			serviceName := ginCtx.Param("service_name")
			var methods []dto.MethodResponse
			for _, method := range s.svcCtx.GrpcSession.Methods {
				if method.GetService().GetFullyQualifiedName() == serviceName {
					methods = append(methods, dto.MethodResponse{
						MethodName: method.GetName(),
					})
				}
			}
			ginCtx.JSON(200, methods)
		}))
		engine.GET("/api/histories", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			serviceName := ginCtx.Query("service_name")
			methodName := ginCtx.Query("method_name")
			var datas []dto.HistoryData
			s.svcCtx.DB.Update(func(tx *bolt.Tx) error {
				tx.CreateBucketIfNotExists([]byte("grpc:service:" + serviceName + methodName))
				return nil
			})
			_ = s.svcCtx.DB.View(func(tx *bolt.Tx) error {
				bucket := tx.Bucket([]byte("grpc:service:" + serviceName + methodName))
				bucket.ForEach(func(k, v []byte) error {
					historyData := dto.HistoryData{
						Key: string(k),
					}
					json.Unmarshal(v, &historyData)
					datas = append(datas, historyData)
					return nil
				})
				return nil
			})
			sort.SliceStable(datas, func(i, j int) bool {
				return datas[i].Time >= datas[j].Time
			})
			var delDatas []dto.HistoryData
			if len(datas) > 200 {
				delDatas = datas[200:]
				datas = datas[:200]
			}
			for _, data := range delDatas {
				s.svcCtx.DB.Update(func(tx *bolt.Tx) error {
					tx.Bucket([]byte("grpc:service:" + serviceName + methodName)).Delete([]byte(data.Key))
					return nil
				})
			}
			var histories []dto.HistoryResponse
			for _, data := range datas {
				historyResponse := dto.HistoryResponse{
					Description:     time.Unix(data.Time, 0).Format("01-02 15:04:05"),
					Interval:        data.Interval,
					Request:         data.Request.RequestData,
					RequestHeaders:  data.Request.Headers,
					Response:        data.Response.Data,
					Status:          data.Response.Status,
					ResponseHeaders: data.Response.Headers,
					Trailers:        data.Response.Trailers,
				}
				histories = append(histories, historyResponse)
			}
			ginCtx.JSON(200, histories)
		}))
		engine.GET("/api/metadata", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			serviceName := ginCtx.Query("service_name")
			methodName := ginCtx.Query("method_name")
			var results *grpcurl.Schema
			var err error
			for _, method := range s.svcCtx.GrpcSession.Methods {
				if method.GetName() == methodName {
					results, err = grpcurl.GatherMetadataForMethod(method)
					break
				}
			}
			if err != nil {
				ginCtx.JSON(500, "Failed to gather metadata for RPC Method: "+err.Error())
				return
			}
			ginCtx.JSON(200, gin.H{
				"service_name": serviceName,
				"method_name":  methodName,
				"request":      results,
			})
		}))
		engine.GET("/api/metrics", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			ginCtx.JSON(200, findMetricsByFeature(s.svcCtx.TaskDB, s.svcCtx.Cfg.Feature))
		}))
		engine.POST("/api/invoke", s.server.MiddleHandle(func(ctx context.Context, ginCtx *gin.Context) {
			start := time.Now()
			lock := sync.RWMutex{}
			request := dto.InvokerRequest{}
			ginCtx.BindJSON(&request)
			var desc grpcurl2.DescriptorSource
			method := request.Method
			for _, method := range s.svcCtx.GrpcSession.Methods {
				if method.GetName() == request.Method {
					request.Method = method.GetFullyQualifiedName()
					var err error
					desc, err = grpcurl2.DescriptorSourceFromFileDescriptors(method.GetFile())
					if err != nil {
						ginCtx.JSON(500, "Failed to create descriptor source: "+err.Error())
						return
					}
					break
				}
			}
			if request.RequestData == "" {
				request.RequestData = "{}"
			}
			wait := sync.WaitGroup{}
			wait.Add(request.RequestConcurrent)
			times := make(chan struct{}, request.RequestConcurrent*100)
			var invokeErr error
			var invokeResp *dto.InvokerResponse
			faileds := 0
			for i := 0; i < request.RequestConcurrent; i++ {
				go func() {
					defer wait.Done()
					for range times {
						if resp, err := grpcurl.InvokeRPC(context.TODO(), s.svcCtx.GrpcSession.C, desc, request, nil); err != nil {
							lock.Lock()
							faileds++
							if invokeErr == nil {
								invokeErr = err
							}
							if invokeResp == nil {
								invokeResp = resp
							}
							lock.Unlock()
						} else {
							lock.Lock()
							if invokeResp == nil {
								invokeResp = resp
							}
							lock.Unlock()
						}
					}
				}()
			}
			for i := 0; i < request.RequestTimes; i++ {
				times <- struct{}{}
			}
			close(times)
			wait.Wait()
			invokeResp.Metrics = dto.Metric{
				Success:     int64(request.RequestTimes - faileds),
				Failed:      int64(faileds),
				SuccessRate: fmt.Sprintf("%.2f%%", float32((request.RequestTimes-faileds)/request.RequestTimes)*100),
				ElapsedTime: fmt.Sprintf("%dms", time.Now().Sub(start).Milliseconds()),
				AverageTime: fmt.Sprintf("%dms", time.Now().Sub(start).Milliseconds()/int64(request.RequestTimes)),
			}
			s.svcCtx.DB.Update(func(tx *bolt.Tx) error {
				if bucket, err := tx.CreateBucketIfNotExists([]byte("grpc:service:" + request.ServiceName + method)); err == nil {
					hash := sha1.New()
					requestData := strings.ReplaceAll(request.RequestData, " ", "")
					requestData = strings.ReplaceAll(request.RequestData, "\n", "")
					headers := strings.ReplaceAll(request.Headers, " ", "")
					headers = strings.ReplaceAll(request.Headers, "\n", "")
					hash.Write([]byte(request.ServiceName + request.Method + requestData + headers))
					key := hex.EncodeToString(hash.Sum(nil))
					historyData := dto.HistoryData{
						Request:  request,
						Response: invokeResp,
						Interval: fmt.Sprintf("%dms", time.Now().Sub(start).Milliseconds()),
						Time:     time.Now().Unix(),
						Status:   invokeResp.Status,
					}
					if invokeResp.Status == 1 {
						historyData.Interval = invokeResp.StatusName + "-" + historyData.Interval
					}
					buff, _ := json.Marshal(historyData)
					_ = bucket.Put([]byte(key), buff)
				}
				return nil
			})
			go func(db *gorm.DB, feature, serviceName, method string, status int) {
				if status == 0 {
					status = 2
				}
				saveMetrics(db, feature, serviceName, method, status)
			}(s.svcCtx.TaskDB, s.svcCtx.Cfg.Target, request.ServiceName, method, invokeResp.Status)
			ginCtx.JSON(200, invokeResp)
		}))
	})
}

func saveMetrics(db *gorm.DB, feature, serviceName, method string, status int) {
	uid := uuid.NewV4()
	id := uid.String()
	if user, err := user.Current(); err != nil {
		zlog.S.Fatal(err)
	} else {
		db.Exec("INSERT INTO `grpc_metrics` ( `id`, `method`, `author`, `status`, `feature`,`created_at`,`updated_at` ) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `status`=?,updated_at=?",
			id, serviceName+":"+method, user.Username, status, feature, time.Now(), time.Now(), status, time.Now(),
		)
	}
}

func findMetricsByFeature(db *gorm.DB, feature string) []Metrics {
	rows, err := db.Raw("select id,author,method,feature,status,created_at,updated_at from grpc_metrics where feature=?", feature).Rows()
	if err != nil {
		return nil
	}
	var metrics []Metrics
	defer rows.Close()
	for rows.Next() {
		metric := Metrics{}
		rows.Scan(&metric.Id, &metric.Author, &metric.Method, &metric.Feature, &metric.Status, &metric.CreatedAt, &metric.UpdatedAt)
		metrics = append(metrics, metric)
	}
	return metrics
}

type Metrics struct {
	Id        string    `json:"id"`
	Author    string    `json:"author"`
	Method    string    `json:"method"`
	Status    int       `json:"status"`
	Feature   string    `json:"feature"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
