package xcaddy

import "os"

func GetGo() string {
	sdk := os.Getenv("XCADDY_SDK")
	if sdk == "" {
		return "go"
	}
	return sdk
}
