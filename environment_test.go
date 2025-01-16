// Copyright 2020 Matthew Holt
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xcaddy

import (
	"context"
	"reflect"
	"testing"

	"github.com/caddyserver/xcaddy/internal/utils"
)

func Test_environment_newGoBuildCommand(t *testing.T) {
	type fields struct {
		buildFlags string
	}
	type args struct {
		args []string
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		wantArgs []string
	}{
		{
			name:     "no flags + no args",
			fields:   fields{},
			args:     args{[]string{}},
			wantArgs: []string{utils.GetGo()},
		},

		{
			name:     "no flags + single arg",
			fields:   fields{},
			args:     args{[]string{"build"}},
			wantArgs: []string{utils.GetGo(), "build"},
		},

		{
			name:     "no flags + multi arg",
			fields:   fields{},
			args:     args{[]string{"build", "main.go"}},
			wantArgs: []string{utils.GetGo(), "build", "main.go"},
		},

		{
			name:     "single flag + no arg",
			fields:   fields{"-trimpath"},
			args:     args{[]string{}},
			wantArgs: []string{utils.GetGo(), "-trimpath"},
		},

		{
			name: "multi flag + no arg",
			fields: fields{
				"-ldflags '-w -s -extldflags=-static'",
			},
			args:     args{},
			wantArgs: []string{utils.GetGo(), "-ldflags", "-w -s -extldflags=-static"},
		},

		{
			name: "multi flag + one arg",
			fields: fields{
				"-ldflags '-w -s -extldflags=-static'",
			},
			args:     args{[]string{"build"}},
			wantArgs: []string{utils.GetGo(), "build", "-ldflags", "-w -s -extldflags=-static"},
		},

		{
			name: "multi flags + multi args",
			fields: fields{
				buildFlags: "-ldflags '-w -s -extldflags=-static'",
			},
			args:     args{[]string{"build", "main.go"}},
			wantArgs: []string{utils.GetGo(), "build", "-ldflags", "-w -s -extldflags=-static", "main.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := environment{
				buildFlags: tt.fields.buildFlags,
			}
			if got := env.newGoBuildCommand(context.TODO(), tt.args.args...); !reflect.DeepEqual(got.Args, tt.wantArgs) {
				t.Errorf("(environment.newGoBuildCommand()).Args = %#v, want %#v", got.Args, tt.wantArgs)
			}
		})
	}
}
