//go:build !slides_noemoji

package slides

import (
	"os/exec"
	"strings"
	"testing"
)

// The default build links the embedded provider, and with it the Twemoji data:
// the other half of TestNoEmojiBuildLinksNoTwemoji, so that the test of the tag
// would notice a go list that stopped listing anything.
func TestDefaultBuildLinksTwemoji(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "github.com/go-icons/twemoji\n") {
		t.Error("the default build does not link go-icons/twemoji")
	}
}
