package control

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

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
