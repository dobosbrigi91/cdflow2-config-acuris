package handler_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sts"

	"github.com/mergermarket/cdflow2-config-acuris/internal/handler"
	common "github.com/mergermarket/cdflow2-config-common"
)

type mockAWSClient struct {
	creds map[string]string
}

func (m *mockAWSClient) STSAssumeRole(string) (*sts.Credentials, error) {
	return &sts.Credentials{
		AccessKeyId:     aws.String(m.creds["accessKeyId"]),
		SecretAccessKey: aws.String(m.creds["secretAccessKey"]),
		SessionToken:    aws.String(m.creds["sessionToken"]),
	}, nil
}

func (m *mockAWSClient) GetECRRepoURI(componentName string) (string, error) {
	return fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/%s", handler.AccountID, handler.Region, componentName), nil
}

type mockAWSClientNonexistentRepo struct{}

func (m *mockAWSClientNonexistentRepo) STSAssumeRole(string) (*sts.Credentials, error) {
	return &sts.Credentials{
		AccessKeyId:     aws.String("accessKeyId"),
		SecretAccessKey: aws.String("secretAccessKey"),
		SessionToken:    aws.String("sessionToken"),
	}, nil
}

func (m *mockAWSClientNonexistentRepo) GetECRRepoURI(componentName string) (string, error) {
	return "", fmt.Errorf("no ecr repo for %q", componentName)
}

type mockAWSClientUnableToObtainRole struct {
	errorText string
}

func (m *mockAWSClientUnableToObtainRole) STSAssumeRole(string) (*sts.Credentials, error) {
	return nil, fmt.Errorf(m.errorText)
}

func (m *mockAWSClientUnableToObtainRole) GetECRRepoURI(string) (string, error) {
	panic("implement me")
}

func TestConfigureRelease(t *testing.T) {
	t.Run("aws creds happy path", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		request.ReleaseRequirements = map[string]map[string]interface{}{
			"build1": {},
			"build2": {},
		}
		response := common.CreateConfigureReleaseResponse()

		assumeRoleCreds := make(map[string]string)
		assumeRoleCreds["accessKeyId"] = "AccessKeyId"
		assumeRoleCreds["secretAccessKey"] = "SecretAccessKey"
		assumeRoleCreds["sessionToken"] = "SessionToken"
		h, _ := createStandardHandler(assumeRoleCreds)

		// When
		h.ConfigureRelease(request, response)

		// Then
		if !response.Success {
			t.Fatal("unexpected failure")
		}
		if len(response.Env) != 2 {
			t.Fatalf("Expected 2 builds, got %d", len(response.Env))
		}
		expectedEnvVars := map[string]string{
			"AWS_ACCESS_KEY_ID":     assumeRoleCreds["accessKeyId"],
			"AWS_DEFAULT_REGION":    handler.Region,
			"AWS_SECRET_ACCESS_KEY": assumeRoleCreds["secretAccessKey"],
			"AWS_SESSION_TOKEN":     assumeRoleCreds["sessionToken"],
		}
		for id := range response.Env {
			if id != "build1" && id != "build2" {
				t.Fatalf("Unexpected build env %q", id)
			}
			if !reflect.DeepEqual(response.Env[id], expectedEnvVars) {
				t.Fatalf("Expected %+v, got %+v", expectedEnvVars, response.Env[id])
			}
		}
	})

	t.Run("Lambda build", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		request.ReleaseRequirements = map[string]map[string]interface{}{
			"my-lambda": {
				"needs": []string{"lambda"},
			},
			"my-x": {},
		}
		response := common.CreateConfigureReleaseResponse()

		h, _ := createStandardHandler(getIrrelevantCreds())

		// When
		h.ConfigureRelease(request, response)

		// Then
		if !response.Success {
			t.Fatal("unexpected failure")
		}
		if len(response.Env) != 2 {
			t.Fatalf("Expected 2 builds, got %d", len(response.Env))
		}
		bucketName := response.Env["my-lambda"]["LAMBDA_BUCKET"]
		if bucketName != handler.DefaultLambdaBucket {
			t.Fatalf("got %q, want %q", bucketName, handler.DefaultLambdaBucket)
		}
		bucketName = response.Env["my-x"]["LAMBDA_BUCKET"]
		if bucketName != "" {
			t.Fatalf("my-x should not have LAMBDA_BUCKET, but got %q", bucketName)
		}
	})

	t.Run("ECR build", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		request.Component = "my-component"
		request.ReleaseRequirements = map[string]map[string]interface{}{
			"my-ecr": {
				"needs": []string{"ecr"},
			},
			"my-x": {},
		}
		response := common.CreateConfigureReleaseResponse()
		h, _ := createStandardHandler(getIrrelevantCreds())

		// When
		h.ConfigureRelease(request, response)

		// Then
		if !response.Success {
			t.Fatal("unexpected failure")
		}
		if len(response.Env) != 2 {
			t.Fatalf("Expected 2 builds, got %d", len(response.Env))
		}
		ecrRepository := response.Env["my-ecr"]["ECR_REPOSITORY"]
		expectedRepository := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com/my-component", handler.AccountID, handler.Region)
		if ecrRepository != expectedRepository {
			t.Fatalf("got %q, want %q", ecrRepository, expectedRepository)
		}
	})

	t.Run("ECR build for nonexistent repo", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		request.Component = "nonexistent-component"
		request.ReleaseRequirements = map[string]map[string]interface{}{
			"my-ecr": {
				"needs": []string{"ecr"},
			},
		}
		response := common.CreateConfigureReleaseResponse()
		var errorBuffer bytes.Buffer
		h := handler.New(&handler.Opts{
			AWSClientFactory: func(map[string]string) handler.AWSClient {
				return &mockAWSClientNonexistentRepo{}
			},
			ErrorStream: &errorBuffer,
		})

		// When
		h.ConfigureRelease(request, response)

		// Then
		if response.Success {
			t.Fatal("unexpected success")
		}
		expectedMessage := fmt.Sprintf("no ecr repo for %q\n", request.Component)
		if errorBuffer.String() != expectedMessage {
			t.Fatalf("expected %q, got %q", expectedMessage, errorBuffer.String())
		}
	})

	t.Run("unsupported need for a build", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		request.ReleaseRequirements = map[string]map[string]interface{}{
			"something": {
				"needs": []string{"unsupported"},
			},
		}
		response := common.CreateConfigureReleaseResponse()

		h, errorBuffer := createStandardHandler(getIrrelevantCreds())

		// When
		h.ConfigureRelease(request, response)

		// Then
		if response.Success {
			t.Fatal("unexpected success")
		}
		if errorBuffer.String() != "unable to satisfy \"unsupported\" need for \"something\" build" {
			t.Fatalf("wrong error?: %q", errorBuffer.String())
		}
	})

	// @todo move it to aws client unit test
	//t.Run("aws credentials failing when creating AWS session on the fly", func(t *testing.T) {
	//	// Given
	//	request := common.CreateConfigureReleaseRequest()
	//	response := common.CreateConfigureReleaseResponse()
	//
	//	errorText := "test-error-text"
	//	var errorBuffer bytes.Buffer
	//	h := handler.New(&handler.Opts{
	//		STSClientFactory: func(map[string]string) (stsiface.STSAPI, error) {
	//			return nil, fmt.Errorf(errorText)
	//		},
	//		AWSClientFactory:
	//		ErrorStream: &errorBuffer,
	//	})
	//
	//	// When
	//	h.ConfigureRelease(request, response)
	//
	//	// Then
	//	if response.Success {
	//		t.Fatal("unexpected success")
	//	}
	//	if errorBuffer.String() != errorText+"\n" {
	//		t.Fatalf("expected %q, got %q", errorText+"\n", errorBuffer.String())
	//	}
	//})

	t.Run("aws credentials failing client to assume role", func(t *testing.T) {
		// Given
		request := common.CreateConfigureReleaseRequest()
		response := common.CreateConfigureReleaseResponse()

		errorText := "test-error-text"
		var errorBuffer bytes.Buffer
		h := handler.New(&handler.Opts{
			AWSClientFactory: func(map[string]string) handler.AWSClient {
				return &mockAWSClientUnableToObtainRole{errorText: errorText}
			},
			ErrorStream: &errorBuffer,
		})

		// When
		h.ConfigureRelease(request, response)

		// Then
		if response.Success {
			t.Fatal("unexpected success")
		}
		fullMessage := "Unable to assume role: " + errorText + "\n"
		if errorBuffer.String() != fullMessage {
			t.Fatalf("expected %q, got %q", fullMessage, errorBuffer.String())
		}
	})
}

func createStandardHandler(assumeRoleCreds map[string]string) (*handler.Handler, *bytes.Buffer) {
	var errorBuffer bytes.Buffer
	return handler.New(&handler.Opts{
		AWSClientFactory: func(map[string]string) handler.AWSClient {
			return &mockAWSClient{creds: assumeRoleCreds}
		},
		ErrorStream: &errorBuffer,
	}), &errorBuffer
}

func getIrrelevantCreds() map[string]string {
	return map[string]string{
		"accessKeyId":     "AccessKeyId",
		"secretAccessKey": "SecretAccessKey",
		"sessionToken":    "SessionToken",
	}
}
