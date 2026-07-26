package client_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/cca2878/gtlv-go/pkg/client"
	"github.com/cca2878/gtlv-go/pkg/solver"
)

// 展示可复用客户端的典型用法：配置一次，对多个 gt/challenge 反复求解，并按类型化错误决策。
func ExampleV3Client_GetValidate() {
	// 点选需要本地求解器（滑动不需要）。模型已内嵌，无需任何外部文件；整个进程建一次即可。
	sol, err := solver.NewCaptchaSolver()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = sol.Close() }()

	// 客户端可复用、并发安全；失败最多重试到 3 次（每次换新图）。
	c := client.NewV3Client(client.WithMaxAttempts(3))

	// gt/challenge 来自你的业务接口；此处仅示意。
	gt, challenge := "your-gt", "your-challenge"

	validate, err := c.GetValidate(context.Background(), gt, challenge, sol)
	switch {
	case err == nil:
		fmt.Println("validate:", validate)
	case errors.Is(err, client.ErrVerificationFailed):
		// 已按 WithMaxAttempts 重试仍未通过。
		log.Println("captcha rejected after retries:", err)
	default:
		log.Println("error:", err)
	}
}

// 展示如何用 errors.As 取出验证失败的服务端原因。
func ExampleVerifyError() {
	var err error = &client.VerifyError{Result: "fail"}

	var ve *client.VerifyError
	if errors.As(err, &ve) {
		fmt.Println("result:", ve.Result)
	}
	fmt.Println("is ErrVerificationFailed:", errors.Is(err, client.ErrVerificationFailed))
	// Output:
	// result: fail
	// is ErrVerificationFailed: true
}
