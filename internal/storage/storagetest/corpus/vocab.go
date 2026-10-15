package corpus

// The vocabularies every corpus draws from. They are fixed: changing one
// changes every corpus built after, so a change here is a change to the
// baseline.

// Profiles are the ProfileIDs objects spread over, most to least common.
var Profiles = []string{"personal", "work", "acme"}

var firstNames = []string{
	"Aisha", "Amir", "Ana", "Ben", "Carla", "Chen", "Dana", "David", "Elena", "Emeka",
	"Farah", "Felix", "Grace", "Hana", "Ivan", "Jade", "Jonas", "Julia", "Karim", "Lena",
	"Liam", "Lucia", "Marco", "Maya", "Mei", "Nadia", "Noah", "Olga", "Omar", "Paulo",
	"Priya", "Rafael", "Rosa", "Sam", "Sara", "Sofia", "Tariq", "Tomas", "Yara", "Zoe",
}

var lastNames = []string{
	"Adams", "Ahmed", "Alves", "Baker", "Bauer", "Becker", "Chen", "Costa", "Diaz", "Dubois",
	"Fischer", "Garcia", "Haddad", "Hansen", "Ibrahim", "Jensen", "Khan", "Kim", "Kowalski", "Lee",
	"Lopez", "Martin", "Meyer", "Moreau", "Nakamura", "Novak", "Okafor", "Park", "Patel", "Rossi",
	"Santos", "Schmidt", "Silva", "Singh", "Smith", "Tanaka", "Walker", "Weber", "Yilmaz", "Zhang",
}

var orgs = []string{
	"Acme Robotics", "Blue Harbor Capital", "Cedar Health", "Delta Freight", "Evergreen Studio",
	"Fjord Analytics", "Granite Legal", "Helix Biotech", "Ion Energy", "Juniper Media",
	"Kestrel Aerospace", "Lumen Labs", "Meridian Bank", "Northwind Logistics", "Orbit Software",
	"Pioneer Foods", "Quartz Security", "Ridge Mobility", "Summit Education", "Tidal Marine",
}

var roles = []string{
	"Founder", "CEO", "CTO", "Head of Ops", "Head of Sales", "Product Manager",
	"Engineering Manager", "Designer", "Data Scientist", "Account Executive", "Recruiter",
	"Investor", "Journalist", "Lawyer", "Consultant", "Researcher",
}

var cities = []string{
	"Lisbon", "Montreal", "Berlin", "Austin", "Toronto", "Amsterdam", "Madrid", "Dublin",
	"Oslo", "Zurich", "Boston", "Denver", "Seattle", "Singapore", "Tokyo", "Sydney",
	"Cape Town", "Nairobi", "Mexico City", "Buenos Aires",
}

// topics are single words so each is one full-text token.
var topics = []string{
	"pricing", "roadmap", "hiring", "onboarding", "migration", "security", "budget", "launch",
	"retention", "contracts", "analytics", "design", "support", "compliance", "fundraising",
	"partnership", "infrastructure", "marketing", "procurement", "training", "research",
	"logistics", "localization", "licensing",
}

// noteKinds are the metadata.type values notes take, as the
// structured_metadata step writes them.
var noteKinds = []string{"observation", "task", "decision", "meeting"}

// sourceTypes are the metadata.source_type values notes take.
var sourceTypes = []string{"text", "slack", "email", "meeting"}

var (
	contactTags = []string{"work", "personal", "client", "vendor", "investor"}
	noteTags    = []string{"work", "personal", "client", "followup", "idea", "urgent", "archive", "meeting"}
)

// sentences are the note body templates. {name} and {org}, {role} and
// {city} are the first mentioned contact's, {name2} the second's name,
// {topic} and {topic2} the note's first two topics.
var sentences = []string{
	"Met {name} to go over the {topic} plan; {org} wants the revised numbers before the next review.",
	"{name} flagged that {topic} is slipping and asked {name2} for a decision this week.",
	"Follow up with {name} on {topic}: {org} is weighing an alternative vendor.",
	"{name2} agreed to own the {topic2} rollout once {name} signs off on the budget.",
	"Lunch in {city} with {name}, now {role} at {org}; {topic} came up twice.",
	"Action for me: send {name} the {topic} summary and loop in {name2} on {topic2}.",
	"{name} is doubtful about the {topic} timeline; the {org} board meets next month.",
	"Decision: pause the {topic2} work until {name} confirms the {org} contract.",
	"Call with {name} about {topic}; agreed to revisit {topic2} after the {city} trip.",
	"{name2} introduced {name} as the contact for {topic} at {org}.",
}
