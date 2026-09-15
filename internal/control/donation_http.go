package control

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/response"
)

const donationIdentityContextKey = "gpt-load.donation.identity"

func (s *Server) authenticateDonation() gin.HandlerFunc {
	return func(c *gin.Context) {
		setSecretResponseHeaders(c)
		if !s.donationsEnabled || s.service == nil {
			writeServiceError(c, "donation_auth", app_errors.ErrDonationUnavailable)
			c.Abort()
			return
		}
		header := c.GetHeader("Authorization")
		fields := strings.Fields(header)
		if len(header) > 512 || len(c.Request.Header.Values("Authorization")) != 1 || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			writeServiceError(c, "donation_auth", app_errors.ErrUnauthorized)
			c.Abort()
			return
		}
		digest := sha256.Sum256([]byte(fields[1]))
		if subtle.ConstantTimeCompare(digest[:], s.donationAuthDigest[:]) != 1 {
			writeServiceError(c, "donation_auth", app_errors.ErrUnauthorized)
			c.Abort()
			return
		}
		identity, err := s.service.DonationCapabilities(c.Request.Context())
		if err != nil {
			writeServiceError(c, "donation_auth", err)
			c.Abort()
			return
		}
		c.Set(donationIdentityContextKey, identity)
		c.Next()
	}
}

func donationRequestIdentity(c *gin.Context) (DonationCapabilities, bool) {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeServiceError(c, "donation_request", app_errors.ErrBadRequest)
		return DonationCapabilities{}, false
	}
	value, exists := c.Get(donationIdentityContextKey)
	identity, valid := value.(DonationCapabilities)
	if !exists || !valid {
		writeServiceError(c, "donation_request", app_errors.ErrUnauthorized)
		return DonationCapabilities{}, false
	}
	return identity, true
}

func (s *Server) handleDonationCapabilities(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	response.SuccessI18n(c, "common.success", identity)
}

func (s *Server) handleDonationGroups(c *gin.Context) {
	if _, ok := donationRequestIdentity(c); !ok {
		return
	}
	result, err := s.service.ListDonationGroups(c.Request.Context())
	if err != nil {
		writeServiceError(c, "donation_groups", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}

func bindDonationJSON(c *gin.Context, target any) error {
	if c.Request.ContentLength > donationMaxBodyBytes {
		return &http.MaxBytesError{Limit: donationMaxBodyBytes}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, donationMaxBodyBytes)
	return bindStrictJSON(c, target)
}

func (s *Server) handleDonationBatchCreate(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	var request DonationBatchRequest
	if err := bindDonationJSON(c, &request); err != nil {
		writeServiceError(c, "donation_batch_create", mapControlJSONError(err))
		return
	}
	key, ok := requiredIdempotencyKey(c, "donation_batch_create")
	if !ok {
		return
	}
	if key != request.BatchID {
		writeServiceError(c, "donation_batch_create", app_errors.ErrInvalidIdempotencyKey)
		return
	}
	result, err := s.service.ReceiveDonationBatch(c.Request.Context(), identity.SourceID, request)
	if err != nil {
		writeServiceError(c, "donation_batch_create", err)
		return
	}
	setMutationResourceLocator(c, request.BatchID)
	response.SuccessI18n(c, "common.success", result)
}

func (s *Server) handleDonationBatchGet(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	result, err := s.service.GetDonationBatch(c.Request.Context(), identity.SourceID, c.Param("batch_id"))
	if err != nil {
		writeServiceError(c, "donation_batch_get", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}

func (s *Server) handleDonationBatchRetry(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	var request DonationRetryRequest
	if err := bindDonationJSON(c, &request); err != nil {
		writeServiceError(c, "donation_batch_retry", mapControlJSONError(err))
		return
	}
	key, ok := requiredIdempotencyKey(c, "donation_batch_retry")
	if !ok {
		return
	}
	result, err := s.service.RetryDonationBatch(c.Request.Context(), identity.SourceID, c.Param("batch_id"), key, request)
	if err != nil {
		writeServiceError(c, "donation_batch_retry", err)
		return
	}
	setMutationResourceLocator(c, c.Param("batch_id"))
	response.SuccessI18n(c, "common.success", result)
}

func (s *Server) handleDonationReviewContext(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	result, err := s.service.GetDonationReviewContext(c.Request.Context(), identity.SourceID,
		c.Param("batch_id"), c.Param("item_id"))
	if err != nil {
		writeServiceError(c, "donation_review_context", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}

func (s *Server) handleDonationReviewActionCreate(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	var request DonationReviewActionRequest
	if err := bindDonationJSON(c, &request); err != nil {
		writeServiceError(c, "donation_review_action", mapControlJSONError(err))
		return
	}
	key, ok := requiredIdempotencyKey(c, "donation_review_action")
	if !ok {
		return
	}
	if key != request.ActionID {
		writeServiceError(c, "donation_review_action", app_errors.ErrInvalidIdempotencyKey)
		return
	}
	result, err := s.service.ApplyDonationReviewAction(c.Request.Context(), identity.SourceID,
		c.Param("batch_id"), c.Param("item_id"), request)
	if err != nil {
		writeServiceError(c, "donation_review_action", err)
		return
	}
	setMutationResourceLocator(c, result.ActionID)
	response.SuccessI18n(c, "common.success", result)
}

func (s *Server) handleDonationReviewActionGet(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	result, err := s.service.GetDonationReviewAction(c.Request.Context(), identity.SourceID, c.Param("action_id"))
	if err != nil {
		writeServiceError(c, "donation_review_action_get", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}

// handleDonationTestCreate runs one administrator-issued call against this
// item's staged key. A streaming request is answered as the contract's
// meta/delta/done event sequence; the raw upstream bytes never leave gpt-load.
func (s *Server) handleDonationTestCreate(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	var request DonationTestRequest
	if c.Request.ContentLength > donationMaxTestRequestBytes {
		writeServiceError(c, "donation_test_create", app_errors.ErrRequestTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, donationMaxTestRequestBytes)
	if err := bindStrictJSON(c, &request); err != nil {
		writeServiceError(c, "donation_test_create", mapControlJSONError(err))
		return
	}
	key, ok := requiredIdempotencyKey(c, "donation_test_create")
	if !ok {
		return
	}
	if key != request.TestID {
		writeServiceError(c, "donation_test_create", app_errors.ErrInvalidIdempotencyKey)
		return
	}
	batchID, itemID := c.Param("batch_id"), c.Param("item_id")
	if !request.Stream {
		result, err := s.service.RunDonationTest(c.Request.Context(), identity.SourceID, batchID, itemID, request, nil, nil)
		if err != nil {
			writeServiceError(c, "donation_test_create", err)
			return
		}
		setMutationResourceLocator(c, result.TestID)
		response.SuccessI18n(c, "common.success", result)
		return
	}
	s.streamDonationTest(c, identity.SourceID, batchID, itemID, request)
}

func (s *Server) streamDonationTest(c *gin.Context, source, batchID, itemID string, request DonationTestRequest) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), donationTestTotalTimeout)
	defer cancel()
	controller := http.NewResponseController(c.Writer)
	started := false
	writeEvent := func(writeCtx context.Context, name string, payload any) error {
		if err := writeCtx.Err(); err != nil {
			return err
		}
		encoded, err := json.Marshal(payload)
		if err != nil || len(encoded)+len(name)+16 > donationMaxEventBytes {
			return app_errors.ErrInternalServer
		}
		deadline := time.Now().Add(donationTestIdleTimeout)
		if contextDeadline, ok := writeCtx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		stop := context.AfterFunc(writeCtx, func() { _ = controller.SetWriteDeadline(time.Now()) })
		defer stop()
		if _, err := c.Writer.WriteString("event: " + name + "\ndata: " + string(encoded) + "\n\n"); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		return writeCtx.Err()
	}
	emitMeta := func(meta DonationTestResult) error {
		setSecretResponseHeaders(c)
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		started = true
		return writeEvent(ctx, "meta", meta)
	}
	sink := func(event donationProjectedEvent) error {
		if event.Text == "" {
			return nil
		}
		writeCtx := event.ctx
		if writeCtx == nil {
			writeCtx = ctx
		}
		return writeEvent(writeCtx, "delta", map[string]string{"text": event.Text})
	}
	result, err := s.service.RunDonationTest(ctx, source, batchID, itemID, request, sink, emitMeta)
	if !started {
		if err != nil {
			writeServiceError(c, "donation_test_create", err)
			return
		}
		// A repeat of a running or completed test returns only its durable JSON
		// metadata. There is no saved response body or second model request.
		response.SuccessI18n(c, "common.success", result)
		return
	}
	if result.FinishedAtMS == 0 {
		// Persistence failed. Ending without done makes the caller report an
		// interrupted stream instead of accepting an unconfirmed terminal fact.
		if err != nil && ctx.Err() == nil {
			logServiceError("donation_test_finish", err, app_errors.ErrInternalServer.Code)
		}
		return
	}
	if err := writeEvent(ctx, "done", result); err != nil {
		cancel()
	}
}

func (s *Server) handleDonationTestGet(c *gin.Context) {
	identity, ok := donationRequestIdentity(c)
	if !ok {
		return
	}
	result, err := s.service.GetDonationTest(c.Request.Context(), identity.SourceID, c.Param("test_id"))
	if err != nil {
		writeServiceError(c, "donation_test_get", err)
		return
	}
	response.SuccessI18n(c, "common.success", result)
}
