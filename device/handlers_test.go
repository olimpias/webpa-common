package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Comcast/webpa-common/logging"
	"github.com/Comcast/webpa-common/wrp"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testTimeout(o *Options, t *testing.T) {
	var (
		assert         = assert.New(t)
		require        = require.New(t)
		request        = httptest.NewRequest("GET", "/", nil)
		response       = httptest.NewRecorder()
		ctx            context.Context
		delegateCalled bool

		handler = alice.New(Timeout(o)).Then(
			http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				delegateCalled = true
				ctx = request.Context()
				assert.NotEqual(context.Background(), ctx)

				deadline, ok := ctx.Deadline()
				assert.False(deadline.IsZero())
				assert.True(deadline.Sub(time.Now()) <= o.requestTimeout())
				assert.True(ok)
			}),
		)
	)

	handler.ServeHTTP(response, request)
	require.True(delegateCalled)

	select {
	case <-ctx.Done():
		// pass
	default:
		assert.Fail("The context should have been cancelled after ServeHTTP exits")
	}
}

func TestTimeout(t *testing.T) {
	t.Run(
		"NilOptions",
		func(t *testing.T) { testTimeout(nil, t) },
	)

	t.Run(
		"DefaultOptions",
		func(t *testing.T) { testTimeout(new(Options), t) },
	)

	t.Run(
		"CustomOptions",
		func(t *testing.T) { testTimeout(&Options{RequestTimeout: 17 * time.Second}, t) },
	)
}

func testUseIDFNilStrategy(t *testing.T) {
	var (
		assert   = assert.New(t)
		request  = httptest.NewRequest("GET", "/", nil)
		response = httptest.NewRecorder()

		handler = alice.New(useID(nil)).Then(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			assert.Fail("The delegate should not have been called")
		}))
	)

	assert.Panics(func() {
		handler.ServeHTTP(response, request)
	})
}

func testUseIDFError(t *testing.T) {
	var (
		assert         = assert.New(t)
		request        = httptest.NewRequest("GET", "/", nil)
		response       = httptest.NewRecorder()
		expectedError  = errors.New("expected")
		strategyCalled bool

		strategy = func(*http.Request) (ID, error) {
			strategyCalled = true
			return invalidID, expectedError
		}

		handler = alice.New(useID(strategy)).Then(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			assert.Fail("The delegate should not have been called")
		}))
	)

	handler.ServeHTTP(response, request)
	assert.True(strategyCalled)
}

func testUseIDFromHeaderMissing(t *testing.T) {
	var (
		assert   = assert.New(t)
		request  = httptest.NewRequest("GET", "/", nil)
		response = httptest.NewRecorder()

		handler = alice.New(UseID.FromHeader).Then(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			assert.Fail("The delegate should not have been called")
		}))
	)

	handler.ServeHTTP(response, request)
}

func testUseIDFromHeader(t *testing.T) {
	var (
		assert         = assert.New(t)
		require        = require.New(t)
		request        = httptest.NewRequest("GET", "/", nil)
		response       = httptest.NewRecorder()
		delegateCalled bool

		handler = alice.New(UseID.FromHeader).Then(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			delegateCalled = true
			id, ok := GetID(request.Context())
			assert.Equal(id, ID("mac:112233445566"))
			assert.True(ok)
		}))
	)

	request.Header.Set(DeviceNameHeader, "mac:112233445566")
	handler.ServeHTTP(response, request)
	require.True(delegateCalled)
}

func testUseIDFromPath(t *testing.T) {
	var (
		assert         = assert.New(t)
		request        = httptest.NewRequest("GET", "/test/mac:112233445566", nil)
		response       = httptest.NewRecorder()
		delegateCalled bool

		handler = alice.New(UseID.FromPath("did")).Then(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			delegateCalled = true
			id, ok := GetID(request.Context())
			assert.Equal(id, ID("mac:112233445566"))
			assert.True(ok)
		}))

		router = mux.NewRouter()
	)

	router.Handle("/test/{did}", handler)
	router.ServeHTTP(response, request)
	assert.Equal(http.StatusOK, response.Code)
	assert.True(delegateCalled)
}

func testUseIDFromPathMissingVars(t *testing.T) {
	var (
		assert   = assert.New(t)
		request  = httptest.NewRequest("GET", "/foo", nil)
		response = httptest.NewRecorder()

		handler = alice.New(UseID.FromPath("did")).Then(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			assert.Fail("The delegate should not have been called")
		}))
	)

	handler.ServeHTTP(response, request)
	assert.Equal(http.StatusBadRequest, response.Code)
}

func testUseIDFromPathMissingDeviceNameVar(t *testing.T) {
	var (
		assert   = assert.New(t)
		request  = httptest.NewRequest("GET", "/foo", nil)
		response = httptest.NewRecorder()

		handler = alice.New(UseID.FromPath("did")).Then(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			assert.Fail("The delegate should not have been called")
		}))

		router = mux.NewRouter()
	)

	router.Handle("/foo", handler)
	router.ServeHTTP(response, request)
	assert.Equal(http.StatusBadRequest, response.Code)
}

func TestUseID(t *testing.T) {
	t.Run("F", func(t *testing.T) {
		t.Run("NilStrategy", testUseIDFNilStrategy)
		t.Run("Error", testUseIDFError)
	})

	t.Run("FromHeader", func(t *testing.T) {
		testUseIDFromHeader(t)
		t.Run("Missing", testUseIDFromHeaderMissing)
	})

	t.Run("FromPath", func(t *testing.T) {
		testUseIDFromPath(t)
		t.Run("MissingVars", testUseIDFromPathMissingVars)
		t.Run("MissingDeviceNameVar", testUseIDFromPathMissingDeviceNameVar)
	})
}

func testMessageHandlerLogger(t *testing.T) {
	var (
		assert = assert.New(t)
		logger = logging.NewTestLogger(nil, t)

		handler = MessageHandler{}
	)

	assert.NotNil(handler.logger())

	handler.Logger = logger
	assert.Equal(logger, handler.logger())
}

func testMessageHandlerServeHTTPDecodeError(t *testing.T) {
	var (
		assert  = assert.New(t)
		require = require.New(t)

		invalidContents    = []byte("this is not a valid WRP message")
		response           = httptest.NewRecorder()
		request            = httptest.NewRequest("GET", "/foo", bytes.NewReader(invalidContents))
		actualResponseBody map[string]interface{}

		router  = new(mockRouter)
		handler = MessageHandler{
			Decoders: wrp.NewDecoderPool(1, wrp.Msgpack),
			Router:   router,
		}
	)

	handler.ServeHTTP(response, request)
	assert.Equal(http.StatusBadRequest, response.Code)
	assert.Equal("application/json", response.HeaderMap.Get("Content-Type"))
	responseContents, err := ioutil.ReadAll(response.Body)
	require.NoError(err)
	assert.NoError(json.Unmarshal(responseContents, &actualResponseBody))

	router.AssertExpectations(t)
}

func testMessageHandlerServeHTTPRouteError(t *testing.T, routeError error, expectedCode int) {
	var (
		assert  = assert.New(t)
		require = require.New(t)

		message = &wrp.Message{
			Type:        wrp.SimpleEventMessageType,
			Source:      "test.com",
			Destination: "mac:123412341234",
		}

		setupEncoders   = wrp.NewEncoderPool(1, wrp.Msgpack)
		requestContents []byte
	)

	require.NoError(setupEncoders.EncodeBytes(&requestContents, message))

	var (
		response           = httptest.NewRecorder()
		request            = httptest.NewRequest("POST", "/foo", bytes.NewReader(requestContents))
		actualResponseBody map[string]interface{}

		router  = new(mockRouter)
		handler = MessageHandler{
			Router:   router,
			Decoders: wrp.NewDecoderPool(1, wrp.Msgpack),
		}
	)

	router.On(
		"Route",
		mock.MatchedBy(func(candidate *Request) bool {
			return candidate.Message != nil &&
				len(candidate.Contents) > 0 &&
				candidate.Format == wrp.Msgpack
		}),
	).Once().Return(nil, routeError)

	handler.ServeHTTP(response, request)
	assert.Equal(expectedCode, response.Code)
	assert.Equal("application/json", response.HeaderMap.Get("Content-Type"))
	responseContents, err := ioutil.ReadAll(response.Body)
	require.NoError(err)
	assert.NoError(json.Unmarshal(responseContents, &actualResponseBody))

	router.AssertExpectations(t)
}

func testMessageHandlerServeHTTPEvent(t *testing.T, requestFormat wrp.Format) {
	var (
		assert  = assert.New(t)
		require = require.New(t)

		event = &wrp.SimpleEvent{
			Source:      "test.com",
			Destination: "mac:123412341234",
			ContentType: "text/plain",
			Payload:     []byte("some lovely data here"),
			Headers:     []string{"Header-1", "Header-2"},
			Metadata:    map[string]string{"foo": "bar"},
		}

		setupEncoders   = wrp.NewEncoderPool(1, requestFormat)
		requestContents []byte
	)

	require.NoError(setupEncoders.EncodeBytes(&requestContents, event))

	var (
		response = httptest.NewRecorder()
		request  = httptest.NewRequest("POST", "/foo", bytes.NewReader(requestContents))

		router  = new(mockRouter)
		handler = MessageHandler{
			Router:   router,
			Decoders: wrp.NewDecoderPool(1, requestFormat),
		}

		actualDeviceRequest *Request
	)

	router.On(
		"Route",
		mock.MatchedBy(func(candidate *Request) bool {
			actualDeviceRequest = candidate
			return candidate.Message != nil &&
				len(candidate.Contents) > 0 &&
				candidate.Format == requestFormat
		}),
	).Once().Return(nil, nil)

	handler.ServeHTTP(response, request)
	assert.Equal(http.StatusOK, response.Code)
	assert.Equal(0, response.Body.Len())
	require.NotNil(actualDeviceRequest)

	router.AssertExpectations(t)
}

func testMessageHandlerServeHTTPRequestResponse(t *testing.T, responseFormat, requestFormat wrp.Format) {
	const transactionKey = "transaction-key"

	var (
		assert  = assert.New(t)
		require = require.New(t)

		requestMessage = &wrp.Message{
			Type:            wrp.SimpleRequestResponseMessageType,
			Source:          "test.com",
			Destination:     "mac:123412341234",
			TransactionUUID: transactionKey,
			ContentType:     "text/plain",
			Payload:         []byte("some lovely data here"),
			Headers:         []string{"Header-1", "Header-2"},
			Metadata:        map[string]string{"foo": "bar"},
		}

		responseMessage = &wrp.Message{
			Type:            wrp.SimpleRequestResponseMessageType,
			Destination:     "test.com",
			Source:          "mac:123412341234",
			TransactionUUID: transactionKey,
		}

		setupRequestEncoders   = wrp.NewEncoderPool(1, requestFormat)
		setupResponseEncoders  = wrp.NewEncoderPool(1, wrp.Msgpack)
		verifyResponseDecoders = wrp.NewDecoderPool(1, responseFormat)
		requestContents        []byte
		responseContents       []byte
	)

	require.NoError(setupRequestEncoders.EncodeBytes(&requestContents, requestMessage))
	require.NoError(setupResponseEncoders.EncodeBytes(&responseContents, responseMessage))

	var (
		response = httptest.NewRecorder()
		request  = httptest.NewRequest("POST", "/foo", bytes.NewReader(requestContents))

		router  = new(mockRouter)
		device  = new(mockDevice)
		handler = MessageHandler{
			Router:   router,
			Decoders: wrp.NewDecoderPool(1, requestFormat),
			Encoders: wrp.NewEncoderPool(1, responseFormat),
		}

		actualDeviceRequest    *Request
		expectedDeviceResponse = &Response{
			Device:   device,
			Message:  responseMessage,
			Format:   wrp.Msgpack,
			Contents: responseContents,
		}
	)

	router.On(
		"Route",
		mock.MatchedBy(func(candidate *Request) bool {
			actualDeviceRequest = candidate
			return candidate.Message != nil &&
				len(candidate.Contents) > 0 &&
				candidate.Format == requestFormat
		}),
	).Once().Return(expectedDeviceResponse, nil)

	handler.ServeHTTP(response, request)
	assert.Equal(http.StatusOK, response.Code)
	assert.Equal(responseFormat.ContentType(), response.HeaderMap.Get("Content-Type"))
	require.NotNil(actualDeviceRequest)
	assert.NoError(verifyResponseDecoders.Decode(new(wrp.Message), response.Body))

	router.AssertExpectations(t)
	device.AssertExpectations(t)
}

func testMessageHandlerServeHTTPEncodeError(t *testing.T) {
	const transactionKey = "transaction-key"

	var (
		assert  = assert.New(t)
		require = require.New(t)

		requestMessage = &wrp.Message{
			Type:            wrp.SimpleRequestResponseMessageType,
			Source:          "test.com",
			Destination:     "mac:123412341234",
			TransactionUUID: transactionKey,
			ContentType:     "text/plain",
			Payload:         []byte("some lovely data here"),
			Headers:         []string{"Header-1", "Header-2"},
			Metadata:        map[string]string{"foo": "bar"},
		}

		responseMessage = &wrp.Message{
			Type:            wrp.SimpleRequestResponseMessageType,
			Destination:     "test.com",
			Source:          "mac:123412341234",
			TransactionUUID: transactionKey,
		}

		setupRequestEncoders = wrp.NewEncoderPool(1, wrp.Msgpack)
		requestContents      []byte
	)

	require.NoError(setupRequestEncoders.EncodeBytes(&requestContents, requestMessage))

	var (
		response = httptest.NewRecorder()
		request  = httptest.NewRequest("POST", "/foo", bytes.NewReader(requestContents))

		router  = new(mockRouter)
		device  = new(mockDevice)
		handler = MessageHandler{
			Router:   router,
			Decoders: wrp.NewDecoderPool(1, wrp.Msgpack),
		}

		actualResponseBody     map[string]interface{}
		expectedDeviceResponse = &Response{
			Device:  device,
			Message: responseMessage,
			Format:  wrp.Msgpack,
		}
	)

	router.On(
		"Route",
		mock.MatchedBy(func(candidate *Request) bool {
			return candidate.Message != nil &&
				len(candidate.Contents) > 0 &&
				candidate.Format == wrp.Msgpack
		}),
	).Once().Return(expectedDeviceResponse, nil)

	handler.ServeHTTP(response, request)
	assert.Equal(http.StatusInternalServerError, response.Code)
	assert.Equal("application/json", response.HeaderMap.Get("Content-Type"))
	responseContents, err := ioutil.ReadAll(response.Body)
	require.NoError(err)
	assert.NoError(json.Unmarshal(responseContents, &actualResponseBody))

	router.AssertExpectations(t)
	device.AssertExpectations(t)
}

func TestMessageHandler(t *testing.T) {
	t.Run("Logger", testMessageHandlerLogger)

	t.Run("ServeHTTP", func(t *testing.T) {
		t.Run("DecodeError", testMessageHandlerServeHTTPDecodeError)
		t.Run("EncodeError", testMessageHandlerServeHTTPEncodeError)

		t.Run("RouteError", func(t *testing.T) {
			testMessageHandlerServeHTTPRouteError(t, ErrorInvalidDeviceName, http.StatusBadRequest)
			testMessageHandlerServeHTTPRouteError(t, ErrorDeviceNotFound, http.StatusNotFound)
			testMessageHandlerServeHTTPRouteError(t, ErrorNonUniqueID, http.StatusBadRequest)
			testMessageHandlerServeHTTPRouteError(t, ErrorInvalidTransactionKey, http.StatusBadRequest)
			testMessageHandlerServeHTTPRouteError(t, ErrorTransactionAlreadyRegistered, http.StatusBadRequest)
			testMessageHandlerServeHTTPRouteError(t, errors.New("random error"), http.StatusInternalServerError)
		})

		t.Run("Event", func(t *testing.T) {
			for _, requestFormat := range []wrp.Format{wrp.Msgpack, wrp.JSON} {
				testMessageHandlerServeHTTPEvent(t, requestFormat)
			}
		})

		t.Run("RequestResponse", func(t *testing.T) {
			for _, responseFormat := range []wrp.Format{wrp.Msgpack, wrp.JSON} {
				for _, requestFormat := range []wrp.Format{wrp.Msgpack, wrp.JSON} {
					testMessageHandlerServeHTTPRequestResponse(t, responseFormat, requestFormat)
				}
			}
		})
	})
}

func testConnectHandlerLogger(t *testing.T) {
	var (
		assert = assert.New(t)
		logger = logging.NewTestLogger(nil, t)

		handler = ConnectHandler{}
	)

	assert.NotNil(handler.logger())

	handler.Logger = logger
	assert.Equal(logger, handler.logger())
}

func testConnectHandlerServeHTTP(t *testing.T, connectError error, responseHeader http.Header) {
	var (
		assert = assert.New(t)

		device    = new(mockDevice)
		connector = new(mockConnector)
		handler   = ConnectHandler{
			Connector:      connector,
			ResponseHeader: responseHeader,
		}

		response = httptest.NewRecorder()
		request  = httptest.NewRequest("GET", "/", nil)
	)

	if connectError != nil {
		connector.On("Connect", response, request, responseHeader).Once().Return(nil, connectError)
	} else {
		device.On("ID").Once().Return(ID("mac:112233445566"))
		connector.On("Connect", response, request, responseHeader).Once().Return(device, connectError)
	}

	handler.ServeHTTP(response, request)

	// the handler itself shouldn't do anything to the response.
	// the Connector does that
	assert.Equal(http.StatusOK, response.Code)

	device.AssertExpectations(t)
	connector.AssertExpectations(t)
}

func TestConnectHandler(t *testing.T) {
	t.Run("Logger", testConnectHandlerLogger)
	t.Run("ServeHTTP", func(t *testing.T) {
		testConnectHandlerServeHTTP(t, nil, nil)
		testConnectHandlerServeHTTP(t, nil, http.Header{"Header-1": []string{"Value-1"}})
		testConnectHandlerServeHTTP(t, errors.New("expected error"), nil)
		testConnectHandlerServeHTTP(t, errors.New("expected error"), http.Header{"Header-1": []string{"Value-1"}})
	})
}

func TestListHandler(t *testing.T) {
	t.Run("ServeHTTP", func(t *testing.T) {
	})
}
