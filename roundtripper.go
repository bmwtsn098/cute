package cute

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/moul/http2curl"
	"github.com/ozontech/allure-go/pkg/allure"
	cuteErrors "github.com/ozontech/cute/errors"
	"github.com/ozontech/cute/internal/utils"
)

func (it *Test) makeRequest(t internalT, req *http.Request) (*http.Response, []error) {
	var (
		delay       = defaultDelayRepeat
		countRepeat = 1

		resp  *http.Response
		err   error
		scope = make([]error, 0)
	)

	if it.Request.Repeat.Delay != 0 {
		delay = it.Request.Repeat.Delay
	}

	if it.Request.Repeat.Count != 0 {
		countRepeat = it.Request.Repeat.Count
	}

	for i := 1; i <= countRepeat; i++ {
		executeWithStep(t, createTitle(i, countRepeat, req), func(t T) []error {
			resp, err = it.doRequest(t, req)
			if err != nil {
				return []error{err}
			}

			return nil
		})

		if err == nil {
			break
		}

		scope = append(scope, err)
		if i != countRepeat {
			time.Sleep(delay)
		}
	}

	return resp, scope
}

func (it *Test) doRequest(t T, baseReq *http.Request) (*http.Response, error) {
	// copy request, because body can be read once
	req, err := copyRequest(baseReq.Context(), baseReq)
	if err != nil {

		return nil, err
	}

	// Add information (method, host, curl) about request to Allure step
	err = addInformationRequest(t, req)
	if err != nil {

		return nil, err
	}

	resp, err := it.httpClient.Do(req)
	if err != nil {

		return nil, err
	}

	if resp != nil {
		// Add information (code, body, headers) about response to Allure step
		addInformationResponse(t, resp)

		err = it.validateResponseCode(t, resp)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (it *Test) validateResponseCode(t T, resp *http.Response) error {
	if it.Expect.Code != 0 && it.Expect.Code != resp.StatusCode {
		return cuteErrors.NewAssertError(
			"Assert response code",
			fmt.Sprintf("Response code expect %v, but was %v", it.Expect.Code, resp.StatusCode),
			resp.StatusCode,
			it.Expect.Code)
	}

	return nil
}

// PrepareStep returns step based on http request.
func addInformationRequest(t T, req *http.Request) error {
	var (
		saveBody io.ReadCloser
		err      error
	)

	curl, err := http2curl.GetCurlCommand(req)
	if err != nil {
		return err
	}

	if c := curl.String(); len(c) <= 2048 {
		t.Log("[Request]" + c)
	}

	headers, err := utils.ToJSON(req.Header)
	if err != nil {
		return err
	}

	t.WithParameters(
		allure.NewParameters(
			"method", req.Method,
			"host", req.Host,
			"headers", headers,
			"curl", curl.String(),
		)...,
	)

	if req.Body != nil {
		saveBody, req.Body, err = utils.DrainBody(req.Body)
		if err != nil {
			return err
		}

		body, err := utils.GetBody(saveBody)
		if err != nil {
			return err
		}

		if len(body) != 0 {
			t.WithNewParameters("body", string(body))
		}
	}

	return nil
}

func copyRequest(ctx context.Context, req *http.Request) (*http.Request, error) {
	var (
		err error

		clone = req.Clone(ctx)
	)

	req.Body, clone.Body, err = utils.DrainBody(req.Body)
	if err != nil {
		return nil, err
	}

	return clone, nil
}

// UpdateStepWithResponse returns step based on already created step and grpc response.
func addInformationResponse(t T, response *http.Response) {
	var (
		saveBody io.ReadCloser
		err      error
	)

	headers, _ := utils.ToJSON(response.Header)
	if headers != "" {
		t.WithNewParameters("response_headers", headers)
	}

	t.WithNewParameters("response_code", fmt.Sprint(response.StatusCode))
	t.Log("[Response] Status: " + response.Status)

	if response.Body == nil {
		return
	}

	saveBody, response.Body, err = utils.DrainBody(response.Body)
	// if could not get body from response, no add to allure
	if err != nil {
		return
	}
	body, err := utils.GetBody(saveBody)
	// if could not get body from response, no add to allure
	if err != nil {
		return
	}

	// if body is empty - skip
	if len(body) == 0 {
		return
	}

	responseType := allure.Text
	if _, ok := response.Header["Content-Type"]; ok {
		if len(response.Header["Content-Type"]) > 0 {
			if strings.Contains(response.Header["Content-Type"][0], "application/json") {
				responseType = allure.JSON
			} else {
				responseType = allure.MimeType(response.Header["Content-Type"][0])
			}
		}
	}
	if responseType == allure.JSON {
		body, _ = utils.PrettyJSON(body)
	}

	t.WithAttachments(allure.NewAttachment("response", responseType, body))
}

func createTitle(try, countRepeat int, req *http.Request) string {
	title := req.Method + " " + req.URL.String()

	if countRepeat == 1 {
		return title
	}

	return fmt.Sprintf("[%v/%v] %v", try, countRepeat, title)
}
