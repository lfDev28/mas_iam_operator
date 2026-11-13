package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

// idpCfgSettle is how long to wait before reading the IDPCfg generation a
// second time, in case the Admin API writes the IDPCfg after its PUT returns.
var idpCfgSettle = 10 * time.Second

// idpCfgGeneration returns the IDPCfg's metadata.generation, or 0 when it
// does not exist yet or cannot be read (0 never counts as unchanged).
func (o *masAuthApplyOptions) idpCfgGeneration(ctx context.Context, client *oc.Client, name string) int64 {
	if !o.wait {
		return 0
	}
	gen, _, err := client.ResourceConditions(ctx, o.masCoreNamespace, "idpcfg", name)
	if err != nil {
		return 0
	}
	return gen
}

// idpCfgUnchanged reports whether the IDPCfg's generation is still before,
// both now and after idpCfgSettle. The IDPCfg CRD has a status subresource,
// so the generation moves only when the spec does: an unchanged generation
// means the PUT changed nothing and MAS has nothing to reconcile.
func idpCfgUnchanged(ctx context.Context, before int64, read func() (int64, error)) bool {
	if before <= 0 {
		return false
	}
	for i := 0; i < 2; i++ {
		if i > 0 && sleepCtx(ctx, idpCfgSettle) != nil {
			return false
		}
		gen, err := read()
		if err != nil || gen != before {
			return false
		}
	}
	return true
}

// waitUnlessUnchanged skips the Ready wait when the PUT left the IDPCfg
// unchanged. Without this, every re-run waited out the stale-Ready grace
// (2 minutes per provider): IDPCfg has no observedGeneration, and its Ready
// condition never moves when there is nothing to reconcile.
func (o *masAuthApplyOptions) waitUnlessUnchanged(ctx context.Context, client *oc.Client, mc *masadmin.Client, kind, idpID, name string, genBefore int64, putAt time.Time, resp *masadmin.IDPConfigResponse) error {
	if !o.wait {
		return nil
	}
	read := func() (int64, error) {
		gen, _, err := client.ResourceConditions(ctx, o.masCoreNamespace, "idpcfg", name)
		return gen, err
	}
	if idpCfgUnchanged(ctx, genBefore, read) {
		fmt.Fprintf(os.Stdout, "[mas-auth] %s/%s unchanged (idpcfg/%s generation %d); nothing for MAS to reconcile\n", kind, idpID, name, genBefore)
		return nil
	}
	return o.maybeWait(ctx, mc, kind, idpID, putAt, resp)
}
