package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/dajee/langhuan/internal/application/service"
)

// queueAdminHandler 提供队列可见性与死信管理端点（platform admin only）。
type queueAdminHandler struct {
	svc *service.QueueAdminService
}

func (h queueAdminHandler) list(c *gin.Context) {
	snapshots, err := h.svc.ListQueues(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, snapshots)
}

func (h queueAdminHandler) listDead(c *gin.Context) {
	queue := c.Param("queue")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, err := h.svc.ListDead(c.Request.Context(), queue, page, pageSize)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

func (h queueAdminHandler) retryDead(c *gin.Context) {
	queue := c.Param("queue")
	taskID := c.Param("task_id")
	if err := h.svc.RetryDead(c.Request.Context(), queue, taskID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"retried": true})
}

func (h queueAdminHandler) deleteDead(c *gin.Context) {
	queue := c.Param("queue")
	taskID := c.Param("task_id")
	if err := h.svc.DeleteDead(c.Request.Context(), queue, taskID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"deleted": true})
}
