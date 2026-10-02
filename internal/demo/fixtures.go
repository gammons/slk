package demo

import (
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

const (
	hr = time.Hour
	mn = time.Minute
)

// Workspaces.
const (
	teamLumen     = "T0LUMEN"
	teamDriftwood = "T0DRIFT"
)

// Lumen Labs people. The user is Alex Rivera in both workspaces, with a
// different ID in each, as in real Slack.
const (
	uAlex   = "U0ALEX"
	uPriya  = "U0PRIYA"
	uSam    = "U0SAM"
	uMaya   = "U0MAYA"
	uJonas  = "U0JONAS"
	uLena   = "U0LENA"
	uDiego  = "U0DIEGO"
	uNoor   = "U0NOOR"
	uTom    = "U0TOM"
	bDeploy = "B0DEPLOY"
)

// Driftwood OSS people.
const (
	uAlexDW = "U1ALEX"
	uRuth   = "U1RUTH"
	uKai    = "U1KAI"
	uElla   = "U1ELLA"
	uOmar   = "U1OMAR"
	uFinn   = "U1FINN"
)

// Conversations.
const (
	chGeneral     = "C0GENERAL"
	chEngineering = "C0ENG"
	chDeploys     = "C0DEPLOYS"
	chDesign      = "C0DESIGN"
	chRandom      = "C0RANDOM"
	chIncidents   = "C0INCIDENTS"
	dmPriya       = "D0PRIYA"
	dmSam         = "D0SAM"
	dmMaya        = "D0MAYA"
	gdmLaunch     = "G0LAUNCH"

	chContributors  = "C1CONTRIB"
	chAnnouncements = "C1ANNOUNCE"
	chHelp          = "C1HELP"
	dmRuth          = "D1RUTH"
)

func fixtureTeams() []teamSpec { return []teamSpec{lumenLabs(), driftwoodOSS()} }

func reacts(emoji string, users ...string) reactSpec { return reactSpec{emoji: emoji, users: users} }

func deployCard(color, title, body string) []blocks.LegacyAttachment {
	return []blocks.LegacyAttachment{{Color: color, Title: title, Text: body, Footer: "deploybot"}}
}

func lumenLabs() teamSpec {
	return teamSpec{
		id: teamLumen, name: "Lumen Labs", domain: "lumen-labs", theme: "tokyo night",
		selfID: uAlex, responder: uPriya, firstChannel: chGeneral,
		users: []user{
			{id: uAlex, name: "Alex Rivera", presence: "active",
				title: "Senior Software Engineer", pronouns: "they/them",
				tz: "America/Los_Angeles", tzAbbrev: "PDT", tzOffset: -25200},
			{id: uPriya, name: "Priya Shah", presence: "active",
				title: "Staff Engineer, Platform", pronouns: "she/her",
				tz: "Asia/Kolkata", tzAbbrev: "IST", tzOffset: 19800},
			{id: uSam, name: "Sam Okafor", presence: "active",
				title: "Backend Engineer", pronouns: "he/him",
				tz: "America/New_York", tzAbbrev: "EDT", tzOffset: -14400},
			{id: uMaya, name: "Maya Chen", presence: "away", status: peerstatus.Status{Emoji: ":art:", Text: "In design review"},
				title: "Product Designer", pronouns: "she/her",
				tz: "America/Los_Angeles", tzAbbrev: "PDT", tzOffset: -25200},
			{id: uJonas, name: "Jonas Berg", presence: "active",
				title: "Site Reliability Engineer", pronouns: "he/him",
				tz: "Europe/Berlin", tzAbbrev: "CEST", tzOffset: 7200},
			{id: uLena, name: "Lena Park", presence: "active",
				title: "Engineering Manager", pronouns: "she/her",
				tz: "America/Los_Angeles", tzAbbrev: "PDT", tzOffset: -25200},
			{id: uDiego, name: "Diego Alvarez", presence: "away",
				title: "Platform Engineer", pronouns: "he/him",
				tz: "America/Mexico_City", tzAbbrev: "CST", tzOffset: -21600},
			{id: uNoor, name: "Noor Haddad", presence: "active",
				title: "Software Engineer, Platform", pronouns: "she/her",
				tz: "Asia/Dubai", tzAbbrev: "GST", tzOffset: 14400},
			{id: uTom, name: "Tom Becker", presence: "away", status: peerstatus.Status{Emoji: ":palm_tree:", Text: "Out until Monday"},
				title: "Head of Operations", pronouns: "he/him",
				tz: "America/Los_Angeles", tzAbbrev: "PDT", tzOffset: -25200},
			{id: bDeploy, name: "deploybot",
				title: "Deployment Bot", tz: "UTC", tzAbbrev: "UTC"},
		},
		channels: []channelSpec{
			{id: chGeneral, name: "general", typ: "channel", section: "Company", sectionOrder: 2, msgs: []msgSpec{
				{user: uLena, ago: 26 * hr, text: "Reminder: all-hands tomorrow at 10am. Agenda is in the doc, add your questions by end of day :memo:",
					reactions: []reactSpec{reacts("eyes", uSam, uTom), reacts("thumbsup", uPriya)}},
				{user: uTom, ago: 25 * hr, text: "The coffee machine on 3 is fixed :coffee: :tada:",
					reactions: []reactSpec{reacts("tada", uPriya, uMaya, uDiego)}},
				{user: uLena, ago: 3 * hr, text: "Please welcome <@U0NOOR> to the platform team! :wave:",
					reactions: []reactSpec{reacts("wave", uPriya, uSam, uMaya, uAlex)}},
				{user: uNoor, ago: 170 * mn, text: "Thanks everyone, excited to be here :blush:"},
				{user: uDiego, ago: 40 * mn, text: "Lunch order goes out at 12:15, tacos today :taco:"},
			}},
			{id: chEngineering, name: "engineering", typ: "channel", section: "Engineering", sectionOrder: 1, unread: 2, mentions: 1, msgs: []msgSpec{
				{user: uJonas, ago: 27 * hr, text: "Heads up: bumping the Go toolchain to 1.26 on main this afternoon."},
				{user: uPriya, ago: 5 * hr,
					text: "Found the cause of the flaky `TestSessionRefresh`. We compared calendar days across time zones:\n" +
						"```\nfunc sameDay(a, b time.Time) bool {\n\tay, am, ad := a.UTC().Date()\n\tby, bm, bd := b.UTC().Date()\n\treturn ay == by && am == bm && ad == bd\n}\n```",
					reactions: []reactSpec{reacts("eyes", uSam), reacts("raised_hands", uJonas, uAlex)},
					replies: []msgSpec{
						{user: uSam, ago: 5*hr - 5*mn, text: "Nice catch. Can we add a case for the DST boundary?"},
						{user: uPriya, ago: 5*hr - 8*mn, text: "Already on it, pushing in a bit"},
					}},
				{user: uSam, ago: 90 * mn, text: "Rate limiter PR is up: *token bucket per workspace*, 50 req/s burst. Reviews welcome :pray:"},
				{user: uPriya, ago: 30 * mn, text: "<@U0ALEX> could you look at the migration in #412 before standup?"},
			}},
			{id: chDeploys, name: "deploys", typ: "channel", section: "Engineering", sectionOrder: 1, msgs: []msgSpec{
				{user: bDeploy, ago: 6 * hr, legacy: deployCard("good", "Deploy #1481 succeeded", "*api* → staging in 2m 48s")},
				{user: uSam, ago: 4 * hr, text: "Promoting 1481 to production :rocket:", replies: []msgSpec{
					{user: uPriya, ago: 4*hr - 2*mn, text: "Watching the dashboards :eyes:"},
					{user: uSam, ago: 4*hr - 4*mn, text: "Canary at 10%, error rate flat"},
					{user: uJonas, ago: 4*hr - 6*mn, text: "p99 latency up 4ms, well inside budget"},
					{user: uSam, ago: 4*hr - 8*mn, text: "50%"},
					{user: uAlex, ago: 4*hr - 9*mn, text: "LGTM from the API side :thumbsup:"},
					{user: uSam, ago: 4*hr - 11*mn, text: "100%, done :white_check_mark:"},
				}},
				{user: bDeploy, ago: 3*hr + 48*mn, legacy: deployCard("good", "Deploy #1481 succeeded", "*api* → production in 3m 12s")},
				{user: bDeploy, ago: 20 * mn, legacy: deployCard("warning", "Deploy #1482 waiting for approval", "*web* → production, requested by <@U0MAYA>")},
			}},
			{id: chIncidents, name: "incidents", typ: "channel", section: "Engineering", sectionOrder: 1, msgs: []msgSpec{
				{user: uTom, ago: 50 * hr, text: "INC-207 resolved. Queue backlog cleared, postmortem on Thursday.",
					reactions: []reactSpec{reacts("pray", uPriya, uSam)}},
			}},
			{id: chDesign, name: "design", typ: "channel", section: "Company", sectionOrder: 2, unread: 1, msgs: []msgSpec{
				{user: uMaya, ago: 23 * hr, text: "New onboarding illustrations are up in Figma :art:"},
				{user: uMaya, ago: 70 * mn, text: "Signups by week since the new onboarding flow shipped :chart_with_upwards_trend:",
					attachments: []core.Attachment{chartAttachment()},
					reactions:   []reactSpec{reacts("fire", uLena, uTom, uPriya)}},
			}},
			{id: chRandom, name: "random", typ: "channel", section: "Company", sectionOrder: 2, msgs: []msgSpec{
				{user: uDiego, ago: 30 * hr, text: "Who else is running the 10k on Saturday? :runner:"},
				{user: uMaya, ago: 2 * hr, text: "Office plant update: the fern survived the weekend :herb:", edited: true,
					reactions: []reactSpec{reacts("herb", uSam), reacts("joy", uTom)}},
			}},
			{id: dmPriya, name: "Priya Shah", typ: "dm", dmUserID: uPriya, msgs: []msgSpec{
				{user: uPriya, ago: 22 * hr, text: "Thanks for pairing on the session bug today!"},
				{user: uAlex, ago: 21 * hr, text: "Anytime :raised_hands:"},
			}},
			{id: dmSam, name: "Sam Okafor", typ: "dm", dmUserID: uSam, unread: 1, mentions: 1, msgs: []msgSpec{
				{user: uSam, ago: 28 * hr, text: "Rate limiter design doc is ready for a look"},
				{user: uAlex, ago: 27 * hr, text: "Reading it now"},
				{user: uSam, ago: 15 * mn, text: "Got 5 minutes to talk about the limiter config?"},
			}},
			{id: dmMaya, name: "Maya Chen", typ: "dm", dmUserID: uMaya, msgs: []msgSpec{
				{user: uMaya, ago: 26 * hr, text: "Could you check the empty-state copy when you get a sec?"},
				{user: uAlex, ago: 25 * hr, text: "Done, left comments in Figma"},
			}},
			{id: gdmLaunch, name: "Priya, Sam, Maya", typ: "group_dm", msgs: []msgSpec{
				{user: uPriya, ago: 4 * hr, text: "Launch checklist for Thursday is in the doc"},
				{user: uMaya, ago: 3 * hr, text: "Added the screenshots"},
				{user: uSam, ago: 170 * mn, text: "I'll own the deploy :rocket:"},
			}},
		},
	}
}

func driftwoodOSS() teamSpec {
	return teamSpec{
		id: teamDriftwood, name: "Driftwood OSS", domain: "driftwood-oss", theme: "catppuccin latte",
		selfID: uAlexDW, responder: uRuth, firstChannel: chContributors,
		users: []user{
			{id: uAlexDW, name: "Alex Rivera", presence: "active",
				title: "Senior Software Engineer", pronouns: "they/them",
				tz: "America/Los_Angeles", tzAbbrev: "PDT", tzOffset: -25200},
			{id: uRuth, name: "Ruth Adeyemi", presence: "active",
				title: "Maintainer", pronouns: "she/her",
				tz: "Africa/Lagos", tzAbbrev: "WAT", tzOffset: 3600},
			{id: uKai, name: "Kai Nakamura", presence: "active",
				title: "Core Contributor", pronouns: "he/him",
				tz: "Asia/Tokyo", tzAbbrev: "JST", tzOffset: 32400},
			{id: uElla, name: "Ella Moreau", presence: "active",
				title: "Documentation Lead", pronouns: "she/her",
				tz: "Europe/Paris", tzAbbrev: "CEST", tzOffset: 7200},
			{id: uOmar, name: "Omar Siddiqui", presence: "active",
				title: "Core Contributor", pronouns: "he/him",
				tz: "Asia/Karachi", tzAbbrev: "PKT", tzOffset: 18000},
			{id: uFinn, name: "Finn O'Brien", presence: "away",
				title: "Community Manager", pronouns: "he/him",
				tz: "Europe/Dublin", tzAbbrev: "IST", tzOffset: 3600},
		},
		// #contributors first: a workspace switch with no remembered
		// channel lands on Channels[0].
		channels: []channelSpec{
			{id: chContributors, name: "contributors", typ: "channel", section: "Project", sectionOrder: 1, unread: 2, mentions: 1, msgs: []msgSpec{
				{user: uKai, ago: 6 * hr, text: "Opened an RFC for the plugin sandbox, feedback welcome"},
				{user: uElla, ago: 45 * mn, text: "Is anyone on the Windows path bug (#88)? Happy to pick it up"},
				{user: uOmar, ago: 30 * mn, text: "<@U1ALEX> you touched that code last, any pointers?"},
			}},
			{id: chAnnouncements, name: "announcements", typ: "channel", section: "Project", sectionOrder: 1, msgs: []msgSpec{
				{user: uRuth, ago: 28 * hr, text: "*Driftwood v0.9.0* is out! Plugin API, faster indexing and 40+ fixes :tada:",
					reactions: []reactSpec{reacts("tada", uKai, uElla, uOmar, uFinn), reacts("rocket", uKai)}},
			}},
			{id: chHelp, name: "help", typ: "channel", section: "Project", sectionOrder: 1, unread: 1, msgs: []msgSpec{
				{user: uFinn, ago: 2 * hr, text: "How do I point Driftwood at a custom config dir? The docs mention `--config` but it seems to be ignored"},
			}},
			{id: dmRuth, name: "Ruth Adeyemi", typ: "dm", dmUserID: uRuth, msgs: []msgSpec{
				{user: uRuth, ago: 26 * hr, text: "Could you cut the 0.9.1 patch release this week?"},
				{user: uAlexDW, ago: 25 * hr, text: "Yep, Thursday"},
			}},
		},
	}
}
