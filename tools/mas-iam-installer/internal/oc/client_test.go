package oc

import "testing"

func TestSplitManageWorkspaceName(t *testing.T) {
	cases := []struct {
		name          string
		objName       string
		namespace     string
		labels        map[string]string
		wantInstance  string
		wantWorkspace string
		wantOK        bool
	}{
		{
			name:         "labels win over a dashed instance id",
			objName:      "mas-dev-ws1",
			namespace:    "mas-mas-dev-manage",
			labels:       map[string]string{"mas.ibm.com/instanceId": "mas-dev", "mas.ibm.com/workspaceId": "ws1"},
			wantInstance: "mas-dev", wantWorkspace: "ws1", wantOK: true,
		},
		{
			name:         "workspace label alone strips the suffix",
			objName:      "mas-dev-ws1",
			labels:       map[string]string{"mas.ibm.com/workspaceId": "ws1"},
			wantInstance: "mas-dev", wantWorkspace: "ws1", wantOK: true,
		},
		{
			name:         "instance label alone strips the prefix",
			objName:      "mas-dev-my-ws",
			labels:       map[string]string{"mas.ibm.com/instanceId": "mas-dev"},
			wantInstance: "mas-dev", wantWorkspace: "my-ws", wantOK: true,
		},
		{
			name:         "no labels: instance from the mas-{instance}-manage namespace",
			objName:      "mas-dev-ws1",
			namespace:    "mas-mas-dev-manage",
			wantInstance: "mas-dev", wantWorkspace: "ws1", wantOK: true,
		},
		{
			name:         "no labels, unrelated namespace: first-dash fallback",
			objName:      "lfmas-masdev",
			namespace:    "elsewhere",
			wantInstance: "lfmas", wantWorkspace: "masdev", wantOK: true,
		},
		{
			name:    "undecodable name",
			objName: "nodash",
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instance, workspace, ok := SplitManageWorkspaceName(tc.objName, tc.namespace, tc.labels)
			if ok != tc.wantOK || instance != tc.wantInstance || workspace != tc.wantWorkspace {
				t.Fatalf("SplitManageWorkspaceName(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.objName, tc.namespace, instance, workspace, ok, tc.wantInstance, tc.wantWorkspace, tc.wantOK)
			}
		})
	}
}

func TestParseManageWorkspaces(t *testing.T) {
	raw := []byte(`{"items":[
		{"metadata":{"name":"mas-dev-ws1","namespace":"mas-mas-dev-manage","labels":{"mas.ibm.com/instanceId":"mas-dev","mas.ibm.com/workspaceId":"ws1"}}},
		{"metadata":{"name":"lfmas-masdev","namespace":"mas-lfmas-manage"}}
	]}`)
	workspaces, err := ParseManageWorkspaces(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("got %d workspaces", len(workspaces))
	}
	if workspaces[0].InstanceID != "lfmas" || workspaces[0].WorkspaceID != "masdev" {
		t.Fatalf("workspaces[0] = %+v", workspaces[0])
	}
	if workspaces[1].InstanceID != "mas-dev" || workspaces[1].WorkspaceID != "ws1" {
		t.Fatalf("workspaces[1] = %+v", workspaces[1])
	}
}
