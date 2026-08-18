package main

import (
	"reflect"
	"testing"
)

func TestStaleWorktrees(t *testing.T) {
	cases := []struct {
		name   string
		names  []string
		active map[int]bool
		want   []string
	}{
		{
			name:   "nothing stale",
			names:  []string{"romp-1", "romp-2"},
			active: map[int]bool{1: true, 2: true},
			want:   nil,
		},
		{
			name:   "finished jobs stale",
			names:  []string{"romp-1", "romp-2", "romp-3"},
			active: map[int]bool{2: true},
			want:   []string{"romp-1", "romp-3"},
		},
		{
			name:   "non-worktree entries ignored",
			names:  []string{"jobs.db", "logs", "romp-1", ".DS_Store"},
			active: map[int]bool{1: true},
			want:   nil,
		},
		{
			name:   "unparseable romp- name ignored",
			names:  []string{"romp-", "romp-x", "romp-1"},
			active: map[int]bool{},
			want:   []string{"romp-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := staleWorktrees(tc.names, tc.active)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("staleWorktrees(%v, %v) = %v, want %v", tc.names, tc.active, got, tc.want)
			}
		})
	}
}
