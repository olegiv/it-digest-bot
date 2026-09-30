package drupalsec

// Fixtures are trimmed snapshots of https://www.drupal.org/security/all/rss.xml
// captured 2026-09-30, plus handcrafted edge cases.

// liveFeedXML has four live items: two contrib advisories (one Critical), a PSA and a core advisory, newest first as drupal.org serves them.
const liveFeedXML = `<?xml version="1.0" encoding="utf-8" ?><rss version="2.0" xml:base="https://www.drupal.org/security/core/advisories/feed" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>Security advisories</title>
    <link>https://www.drupal.org/security/core/advisories/feed</link>
    <description></description>
    <language>en</language>
     <atom:link href="https://www.drupal.org/security/all/rss.xml/advisories/feed" rel="self" type="application/rss+xml" />
      <item>
    <title>Diba carousel slider - Moderately critical - Cross Site Scripting (XSS) - SA-CONTRIB-2026-191</title>
    <link>https://www.drupal.org/sa-contrib-2026-191</link>
    <description>&lt;div class=&quot;field field-name-field-project field-type-entityreference field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/project/diba_carousel&quot;&gt;Diba carousel slider&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-project-machine-name field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project machine name:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;code&gt;diba_carousel&lt;/code&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-drupalorg-sa-date field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Date:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;2026-September-23&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-criticality field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Security risk:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/security-team/risk-levels&quot; class=&quot;moderately-critical&quot; title=&quot;AC - Access complexity: Basic or routine (user must follow specific path)
A - Authentication: User-level access (basic/commonly assigned permissions)
CI - Confidentiality impact: Certain non-public data is released
II - Integrity impact: Some data can be modified
E - Exploit (Zero-day impact): Theoretical or white-hat (no public exploit code or documentation on development exists)
TD - Target distribution: Only uncommon module configurations are exploitable&quot;&gt;&lt;strong&gt;Moderately critical&lt;/strong&gt; 12 ∕ 25 AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Uncommon&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-type field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Vulnerability:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;Cross Site Scripting (XSS)&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-affected-versions field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Affected versions:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&amp;lt;3.0.2&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-cve field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;CVE IDs:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;CVE-2026-96382&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-description field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Description:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;The Diba Carousel Slider adds a Bootstrap carousel slider block that can be used directly without creating a View or custom integration.&lt;/p&gt;
&lt;p&gt;When the &quot;Allow HTML description&quot; option is enabled, slide descriptions are rendered using the raw stored field value instead of the field&#039;s rendered output. This bypasses Drupal&#039;s text format filtering and output sanitization mechanisms.&lt;/p&gt;
&lt;p&gt;This vulnerability affects sites that use a formatted text field as the carousel description source and have enabled the &quot;Allow HTML description&quot; option.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-solution field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Solution:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;Upgrade to the latest version:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;If you are using Diba carousel slider 3.1.x, upgrade to &lt;a href=&quot;https://www.drupal.org/project/diba_carousel/releases/3.1.0&quot; rel=&quot;nofollow&quot;&gt;Diba carousel slider 3.1.0.&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;If you are using Diba carousel slider 3.0.x, upgrade to &lt;a href=&quot;https://www.drupal.org/project/diba_carousel/releases/3.0.2&quot; rel=&quot;nofollow&quot;&gt;Diba carousel slider 3.0.2.&lt;/a&gt;&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;If you are unable to upgrade immediately, disable the &quot;Allow HTML description&quot; option in affected carousel blocks until the update can be applied.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-reported-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Reported By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/kekkis&quot; rel=&quot;nofollow&quot;&gt;Kalle Kipinä (kekkis)&lt;/a&gt;
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-fixed-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Fixed By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/oriol_e9g&quot; rel=&quot;nofollow&quot;&gt;Oriol Roselló Castells (oriol_e9g)&lt;/a&gt;
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-coordinated-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Coordinated By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/akalata&quot; rel=&quot;nofollow&quot;&gt;Swan Kalata (akalata)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/bramdriesen&quot; rel=&quot;nofollow&quot;&gt;Bram Driesen (bramdriesen)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/greggles&quot; rel=&quot;nofollow&quot;&gt;Greg Knaddison (greggles)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/poker10&quot; rel=&quot;nofollow&quot;&gt;Juraj Nemec (poker10)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/xjm&quot; rel=&quot;nofollow&quot;&gt;Jess  (xjm)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;</description>
     <pubDate>Wed, 23 Sep 2026 17:24:33 +0000</pubDate>
 <dc:creator>Drupal Security Team</dc:creator>
 <guid isPermaLink="false">3623987 at https://www.drupal.org</guid>
  </item>
<item>
    <title>Tawk.to - Live chat application - Critical - Cross Site Request Forgery - SA-CONTRIB-2026-184</title>
    <link>https://www.drupal.org/sa-contrib-2026-184</link>
    <description>&lt;div class=&quot;field field-name-field-project field-type-entityreference field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/project/tawk_to&quot;&gt;Tawk.to - Live chat application&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-project-machine-name field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project machine name:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;code&gt;tawk_to&lt;/code&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-drupalorg-sa-date field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Date:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;2026-September-23&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-criticality field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Security risk:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/security-team/risk-levels&quot; class=&quot;critical&quot; title=&quot;AC - Access complexity: Basic or routine (user must follow specific path)
A - Authentication: None (all/anonymous users)
CI - Confidentiality impact: Certain non-public data is released
II - Integrity impact: Some data can be modified
E - Exploit (Zero-day impact): Theoretical or white-hat (no public exploit code or documentation on development exists)
TD - Target distribution: All module configurations are exploitable&quot;&gt;&lt;strong&gt;Critical&lt;/strong&gt; 16 ∕ 25 AC:Basic/A:None/CI:Some/II:Some/E:Theoretical/TD:All&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-type field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Vulnerability:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;Cross Site Request Forgery&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-affected-versions field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Affected versions:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&amp;lt;3.0.4&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-cve field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;CVE IDs:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;CVE-2026-96388&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-description field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Description:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;This module provides integration of the tawk.to live chat for Drupal sites.&lt;/p&gt;
&lt;p&gt;The module does not sufficiently validate certain requests. This may allow an attacker to trick an authenticated user into performing unintended actions through a Cross-Site Request Forgery (CSRF) vulnerability.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-solution field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Solution:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;Install the latest version:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;Upgrade to &lt;a href=&quot;https://www.drupal.org/project/tawk_to/releases/3.0.4&quot; rel=&quot;nofollow&quot;&gt;tawk.to 3.0.4&lt;/a&gt;.&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;After updating, clear the Drupal cache.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-reported-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Reported By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/s4m0y3d&quot; rel=&quot;nofollow&quot;&gt;Tin Nguyen Huu (s4m0y3d)&lt;/a&gt;
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-fixed-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Fixed By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/andriy-khomych&quot; rel=&quot;nofollow&quot;&gt;Andriy Khomych (andriy khomych)&lt;/a&gt;
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/s4m0y3d&quot; rel=&quot;nofollow&quot;&gt;Tin Nguyen Huu (s4m0y3d)&lt;/a&gt;
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-coordinated-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Coordinated By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/akalata&quot; rel=&quot;nofollow&quot;&gt;Swan Kalata (akalata)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/bramdriesen&quot; rel=&quot;nofollow&quot;&gt;Bram Driesen (bramdriesen)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/damienmckenna&quot; rel=&quot;nofollow&quot;&gt;Damien McKenna (damienmckenna)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/greggles&quot; rel=&quot;nofollow&quot;&gt;Greg Knaddison (greggles)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/poker10&quot; rel=&quot;nofollow&quot;&gt;Juraj Nemec (poker10)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/xjm&quot; rel=&quot;nofollow&quot;&gt;Jess  (xjm)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;</description>
     <pubDate>Wed, 23 Sep 2026 17:14:48 +0000</pubDate>
 <dc:creator>Drupal Security Team</dc:creator>
 <guid isPermaLink="false">3623898 at https://www.drupal.org</guid>
  </item>
<item>
    <title>Upcoming critical contributed project security release on September 23, 2026 - PSA-2026-09-21</title>
    <link>https://www.drupal.org/psa-2026-09-21</link>
    <description>&lt;div class=&quot;field field-name-drupalorg-sa-date field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Date:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;2026-September-21&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-description field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Description:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;em&gt;Updated 2026-09-22, 12:00 UTC, to document that the advisories in the release might be possible for more common configurations, and that other projects may have advisories.&lt;/em&gt;&lt;/li&gt;
&lt;li&gt;&lt;em&gt;Updated 2026-09-21, 19:00 UTC, to document that this is not covered by Drupal Steward.&lt;/em&gt;&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;There will be a security release for a widely used contributed module on &lt;strong&gt;September 23, 2026 between 17:00 and 21:00 UTC&lt;/strong&gt;.&lt;/p&gt;
&lt;p&gt;We are announcing this release in advance because the affected contributed module is used on a significant portion of Drupal sites, and the upcoming release will include a significant number of advisories.&lt;/p&gt;
&lt;p&gt;The advisory with the highest risk score for the release is currently rated as &lt;strong&gt;critical&lt;/strong&gt;. Other, less severe advisories in the release may be accessible to anonymous users, or result from default configurations.&lt;/p&gt;
&lt;p&gt;Other contributed projects may also release advisories on the same date, possibly with more severe vulnerabilities. Drupal core is not affected.&lt;/p&gt;
&lt;h4&gt;Drupal Steward information&lt;/h4&gt;
&lt;p&gt;These releases will not be covered by Drupal Steward.&lt;/p&gt;
&lt;!--break--&gt;&lt;h4&gt;Advisories may be published in batches (a few at a time)
&lt;/h4&gt;&lt;p&gt;The current rate of advisories may require changes to our practices going forward:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;The security team may publish advisories individually, at different times inside the window.&lt;/li&gt;
&lt;li&gt;We will try to publish batches grouped by module.&lt;/li&gt;
&lt;li&gt;We will announce in Slack when all planned releases for the day are complete.&lt;/li&gt;
&lt;li&gt;We will release mailing list emails about the security updates together at the end of the window, to reduce the risk of site owners updating multiple times while advisories are still being published.&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;These changes are intended to make the process easier for the team and to make communication from the team easier to follow.&lt;/p&gt;
&lt;h4&gt;No special release procedures&lt;/h4&gt;
&lt;p&gt;The planned update does not require special release procedures.&lt;/p&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-solution field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Solution:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;&lt;em&gt;Update, 2026-09-23 18:30 UTC&lt;/em&gt;: The &lt;a href=&quot;https://www.drupal.org/project/webform&quot; rel=&quot;nofollow&quot;&gt;Webform project&lt;/a&gt; has released the below 20 advisories today. Make note of the critical advisory &lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-175&quot; rel=&quot;nofollow&quot;&gt;SA-CONTRIB-2026-175&lt;/a&gt;, which has slightly increased severity than was originally noted in this public service announcement.&lt;/p&gt;
&lt;p&gt;Other advisories than those below were published for other projects, so site owners should follow all normal update procedures. &lt;/p&gt;
&lt;ol&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-154&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-154&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-155&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-155&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;(Advisory numbers 156 and 157 were accidentally skipped in how we applied the numbering; these are not missing security advisories.)&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-158&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-158&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-159&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-159&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-160&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-160&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-161&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-161&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-162&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-162&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-163&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-163&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-164&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass, Server-side request forgery - SA-CONTRIB-2026-164&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-165&quot; rel=&quot;nofollow&quot;&gt;Webform - Less critical - Access bypass - SA-CONTRIB-2026-165&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-166&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-166&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-167&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass - SA-CONTRIB-2026-167&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-168&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass - SA-CONTRIB-2026-168&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-169&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass - SA-CONTRIB-2026-169&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-170&quot; rel=&quot;nofollow&quot;&gt;Webform - Less critical - Denial of service - SA-CONTRIB-2026-170&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-171&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass - SA-CONTRIB-2026-171&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-172&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Cross-site scripting - SA-CONTRIB-2026-172&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-173&quot; rel=&quot;nofollow&quot;&gt;Webform - Less critical - Access bypass - SA-CONTRIB-2026-173&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-174&quot; rel=&quot;nofollow&quot;&gt;Webform - Moderately critical - Access bypass - SA-CONTRIB-2026-174&lt;/a&gt;&lt;/li&gt;
&lt;li&gt;&lt;a href=&quot;https://www.drupal.org/sa-contrib-2026-175&quot; rel=&quot;nofollow&quot;&gt;Webform - Critical - Remote Code Execution - SA-CONTRIB-2026-175&lt;/a&gt;&lt;/li&gt;
&lt;/ol&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-coordinated-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Coordinated By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/bramdriesen&quot; rel=&quot;nofollow&quot;&gt;Bram Driesen (bramdriesen)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/damienmckenna&quot; rel=&quot;nofollow&quot;&gt;Damien McKenna (damienmckenna)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/greggles&quot; rel=&quot;nofollow&quot;&gt;Greg Knaddison (greggles)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/longwave&quot; rel=&quot;nofollow&quot;&gt;Dave Long (longwave)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/moshe-weitzman&quot; rel=&quot;nofollow&quot;&gt;Moshe Weitzman (moshe weitzman)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/prudloff&quot; rel=&quot;nofollow&quot;&gt;Pierre Rudloff (prudloff)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/xjm&quot; rel=&quot;nofollow&quot;&gt;Jess  (xjm)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;</description>
     <pubDate>Mon, 21 Sep 2026 09:56:09 +0000</pubDate>
 <dc:creator>Drupal Security Team</dc:creator>
 <guid isPermaLink="false">3624755 at https://www.drupal.org</guid>
  </item>
<item>
    <title>Drupal core - Moderately critical - Third-party libraries - SA-CORE-2026-013</title>
    <link>https://www.drupal.org/sa-core-2026-013</link>
    <description>&lt;div class=&quot;field field-name-field-project field-type-entityreference field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/project/drupal&quot;&gt;Drupal core&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-project-machine-name field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Project machine name:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;code&gt;drupal&lt;/code&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-drupalorg-sa-date field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Date:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;2026-September-16&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-criticality field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Security risk:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;a href=&quot;/security-team/risk-levels&quot; class=&quot;moderately-critical&quot; title=&quot;AC - Access complexity: Basic or routine (user must follow specific path)
A - Authentication: User-level access (basic/commonly assigned permissions)
CI - Confidentiality impact: Certain non-public data is released
II - Integrity impact: Some data can be modified
E - Exploit (Zero-day impact): Theoretical or white-hat (no public exploit code or documentation on development exists)
TD - Target distribution: Default or common module configurations are exploitable, but a config change can disable the exploit&quot;&gt;&lt;strong&gt;Moderately critical&lt;/strong&gt; 13 ∕ 25 AC:Basic/A:User/CI:Some/II:Some/E:Theoretical/TD:Default&lt;/a&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-type field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Vulnerability:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;Third-party libraries&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-affected-versions field-type-text field-label-inline clearfix&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Affected versions:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&amp;gt;=10.5.0 &amp;lt;10.6.17 || &amp;gt;=11.0.0 &amp;lt;11.3.17 || &amp;gt;=11.4.0 &amp;lt;11.4.7&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-description field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Description:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;The Drupal project uses the &lt;a href=&quot;https://github.com/ckeditor/ckeditor5&quot; rel=&quot;nofollow&quot;&gt;CKEditor&lt;/a&gt; library for WYSIWYG editing. CKEditor has released &lt;a href=&quot;https://github.com/ckeditor/ckeditor5/security/advisories/GHSA-rh54-vffm-5fvp&quot; rel=&quot;nofollow&quot;&gt;a security update that impacts Drupal&lt;/a&gt;.&lt;/p&gt;
&lt;p&gt;Vulnerabilities are possible if Drupal is configured to use CKEditor for WYSIWYG editing. An attacker that can create or edit content (even without access to CKEditor themselves) may be able to exploit this Cross-Site Scripting (XSS) vulnerability to target users with access to the WYSIWYG CKEditor, including site admins with privileged access.&lt;/p&gt;
&lt;p&gt;For more information, see CKEditor&#039;s security advisory:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;https://github.com/ckeditor/ckeditor5/security/advisories/GHSA-rh54-vffm-5fvp&quot; rel=&quot;nofollow&quot;&gt;High-severity Cross-site scripting (XSS) in the engine package&lt;/a&gt;&lt;/li&gt;
&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-solution field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Solution:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;p&gt;Install the latest version:&lt;/p&gt;
&lt;p&gt;&lt;strong&gt;Drupal 11&lt;/strong&gt;&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;If you use Drupal 11.4.x, update to &lt;a href=&quot;https://www.drupal.org/project/drupal/releases/11.4.7&quot; rel=&quot;nofollow&quot;&gt;Drupal 11.4.7&lt;/a&gt;.&lt;/li&gt;
&lt;li&gt;If you use Drupal 11.3.x, update to &lt;a href=&quot;https://www.drupal.org/project/drupal/releases/11.3.17&quot; rel=&quot;nofollow&quot;&gt;Drupal 11.3.17&lt;/a&gt;.&lt;/li&gt;
&lt;li&gt;Drupal 11.2.x and below are end-of-life and do not receive security coverage.&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;&lt;strong&gt;Drupal 10&lt;/strong&gt;&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;If you use Drupal 10.6.x, update to &lt;a href=&quot;https://www.drupal.org/project/drupal/releases/10.6.17&quot; rel=&quot;nofollow&quot;&gt;Drupal 10.6.17&lt;/a&gt;.&lt;/li&gt;
&lt;li&gt;Drupal 10.5.x and below are end-of-life and do not receive security coverage.&lt;/li&gt;
&lt;/ul&gt;
&lt;p&gt;Note that &lt;a href=&quot;https://www.drupal.org/psa-2021-06-29&quot; rel=&quot;nofollow&quot;&gt;Drupal 8 &lt;/a&gt; and &lt;a href=&quot;https://www.drupal.org/psa-2023-11-01&quot; rel=&quot;nofollow&quot;&gt;Drupal 9&lt;/a&gt; have both reached end-of-life.&lt;/p&gt;
&lt;h3&gt;Instructions for contributed modules&lt;/h3&gt;
&lt;p&gt;Site owners should also review their site following the &lt;a href=&quot;https://www.drupal.org/psa-2011-002&quot; rel=&quot;nofollow&quot;&gt;protocol for managing external libraries and plugins&lt;/a&gt;, as contributed projects may use additional CKEditor plugins not packaged in Drupal core.&lt;/p&gt;
&lt;p&gt;CKEditor has also released another CVE in today&#039;s release that does not affect Drupal, but may affect custom plugins or other usecases:&lt;/p&gt;
&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;https://github.com/ckeditor/ckeditor5/security/advisories/GHSA-v6mg-96c6-gmpq&quot; rel=&quot;nofollow&quot;&gt;Low-severity Cross-site scripting (XSS) in the engine package&lt;/a&gt;&lt;/li&gt;
&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-reported-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Reported By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/reinmar&quot; rel=&quot;nofollow&quot;&gt;Piotrek Koszuliński (Reinmar)&lt;/a&gt;
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-fixed-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Fixed By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/catch&quot; rel=&quot;nofollow&quot;&gt; catch (catch)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/larowlan&quot; rel=&quot;nofollow&quot;&gt;Lee Rowlands (larowlan)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/mohit_aghera&quot; rel=&quot;nofollow&quot;&gt;Mohit Aghera (mohit_aghera)&lt;/a&gt;, provisional member of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/xjm&quot; rel=&quot;nofollow&quot;&gt;Jess  (xjm)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;&lt;div class=&quot;field field-name-field-sa-coordinated-by field-type-text-long field-label-above&quot;&gt;&lt;div class=&quot;field-label&quot;&gt;Coordinated By:&amp;nbsp;&lt;/div&gt;&lt;div class=&quot;field-items&quot;&gt;&lt;div class=&quot;field-item even&quot;&gt;&lt;ul&gt;
&lt;li&gt;&lt;a href=&quot;/u/bramdriesen&quot; rel=&quot;nofollow&quot;&gt;Bram Driesen (bramdriesen)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/catch&quot; rel=&quot;nofollow&quot;&gt; catch (catch)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/greggles&quot; rel=&quot;nofollow&quot;&gt;Greg Knaddison (greggles)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/larowlan&quot; rel=&quot;nofollow&quot;&gt;Lee Rowlands (larowlan)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/longwave&quot; rel=&quot;nofollow&quot;&gt;Dave Long (longwave)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;li&gt;&lt;a href=&quot;/u/xjm&quot; rel=&quot;nofollow&quot;&gt;Jess  (xjm)&lt;/a&gt; of the Drupal Security Team
&lt;/li&gt;&lt;/ul&gt;&lt;/div&gt;&lt;/div&gt;&lt;/div&gt;</description>
     <pubDate>Wed, 16 Sep 2026 16:21:04 +0000</pubDate>
 <dc:creator>Drupal Security Team</dc:creator>
 <guid isPermaLink="false">3623427 at https://www.drupal.org</guid>
  </item>
  </channel>
</rss>
`

// feedHead is the RSS envelope up to the first item.
const feedHead = `<?xml version="1.0" encoding="utf-8" ?><rss version="2.0" xml:base="https://www.drupal.org/security/core/advisories/feed" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>Security advisories</title>
    <link>https://www.drupal.org/security/core/advisories/feed</link>
    <description></description>
    <language>en</language>
     <atom:link href="https://www.drupal.org/security/all/rss.xml/advisories/feed" rel="self" type="application/rss+xml" />
`

// feedTail closes the envelope.
const feedTail = `
  </channel>
</rss>
`
