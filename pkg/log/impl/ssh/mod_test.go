package ssh

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestGetCommand(t *testing.T) {
	type args struct {
		search *client.LogSearch
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Test with simple command",
			args: args{
				search: &client.LogSearch{
					Options: ty.MI{
						"cmd": "echo 'hello'",
					},
				},
			},
			want: "echo 'hello'",
		},
		{
			name: "Test with size parameter",
			args: args{
				search: &client.LogSearch{
					Size: ty.Opt[int]{Value: 50, Set: true},
					Options: ty.MI{
						"cmd": "tail -n {{.Size.Value}} my-app.log",
					},
				},
			},
			want: "tail -n 50 my-app.log",
		},
		{
			name: "Test with GTE parameter",
			args: args{
				search: &client.LogSearch{
					Range: client.SearchRange{
						Gte: ty.Opt[string]{Value: "2023-10-27T10:00:00Z", Set: true},
					},
					Options: ty.MI{
						"cmd": `grep "{{.Range.Gte.Value}}" my-app.log`,
					},
				},
			},
			want: `grep "2023-10-27T10:00:00Z" my-app.log`,
		},
		{
			name: "Test with default value",
			args: args{
				search: &client.LogSearch{
					Options: ty.MI{
						"cmd": `tail -n {{or .Size.Value 100}}`,
					},
				},
			},
			want: `tail -n 100`,
		},
		{
			name: "Test with all parameters",
			args: args{
				search: &client.LogSearch{
					Size: ty.Opt[int]{Value: 200, Set: true},
					Range: client.SearchRange{
						Last: ty.Opt[string]{Value: "1h", Set: true},
					},
					Options: ty.MI{
						"cmd": `echo "{{.Size.Value}} {{.Range.Last.Value}}"`,
					},
				},
			},
			want: `echo "200 1h"`,
		},
		{
			name: "Test with no command",
			args: args{
				search: &client.LogSearch{
					Options: ty.MI{},
				},
			},
			wantErr: true,
		},
		{
			name: "Test with invalid template",
			args: args{
				search: &client.LogSearch{
					Options: ty.MI{
						"cmd": `echo "{{.Size.Value"`,
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getCommand(tt.args.search)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
