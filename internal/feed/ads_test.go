package feed

import (
	"strings"
	"testing"
)

// Mirrors the two sponsored blocks readability leaves in a BleepingComputer
// article: an inline banner (<p><a><img /c/w/></a></p>) and an end-of-article
// promo <div> (image under /c/p/ + hubs.li CTAs + promo copy), interleaved with
// editorial paragraphs and the real lead image (under /content/).
const bcBody = `<p><img src="https://www.bleepstatic.com/content/hl-images/2026/06/19/texas.jpg"/></p>
<p>The Texas Parks and Wildlife Department disclosed a data breach.</p>
<p><a href="https://www.wiz.io/lp/x?utm_campaign=FY27&amp;utm_term=970x250"><img src="https://www.bleepstatic.com/c/w/secure-vibe-coding-970.jpg" alt="image"/></a></p>
<p>The exposed data set is sufficient for hackers.</p>
<div>
  <p><a href="https://hubs.li/Q04jQ9z40"><img src="https://www.bleepstatic.com/c/p/bas-report.jpg" alt="article image"/></a></p>
  <div>
    <h2><a href="https://hubs.li/Q04jQ9z40">Test every layer before attackers do</a></h2>
    <p>Security teams log 54% of successful attacks.</p>
    <p><a href="https://hubs.li/Q04jQ9z40">Get the whitepaper</a></p>
  </div>
</div>
<p>TPWD advises customers to monitor their credit reports.</p>`

func TestStripPublisherAds_BleepingComputer(t *testing.T) {
	out := StripPublisherAds(bcBody, "https://www.bleepingcomputer.com/news/security/texas-govt-data-breach/")

	// Both ad images and the CTA links must be gone.
	for _, gone := range []string{"bleepstatic.com/c/", "hubs.li", "wiz.io", "Test every layer", "Security teams log 54%", "Get the whitepaper"} {
		if strings.Contains(out, gone) {
			t.Errorf("ad content not removed: %q still present\n%s", gone, out)
		}
	}
	// Editorial content and the real lead image must survive.
	for _, kept := range []string{"Texas Parks and Wildlife", "sufficient for hackers", "monitor their credit reports", "bleepstatic.com/content/"} {
		if !strings.Contains(out, kept) {
			t.Errorf("editorial content removed: %q missing\n%s", kept, out)
		}
	}
}

func TestStripPublisherAds_NonCuratedHostUnchanged(t *testing.T) {
	if got := StripPublisherAds(bcBody, "https://example.com/post"); got != bcBody {
		t.Errorf("body altered for an uncurated host:\n%s", got)
	}
}
