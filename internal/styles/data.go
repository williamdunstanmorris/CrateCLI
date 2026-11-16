// Package styles holds Discogs' genre → style taxonomy.
//
// Discogs has no API endpoint that lists styles, so the list is embedded.
// Every name here was checked against the live search API in Nov 2026
// (style=<name> returns releases tagged with exactly that style), and the
// list was topped up with styles harvested from real search results.
package styles

// Taxonomy maps each Discogs genre to its styles, in display order.
var Taxonomy = []Genre{
	{"Electronic", []string{
		"Abstract", "Acid", "Acid House", "Acid Jazz", "Ambient", "Ambient House", "Avantgarde",
		"Balearic", "Bassline", "Beatdown", "Berlin-School", "Big Beat", "Bleep", "Breakbeat",
		"Breakcore", "Breaks", "Broken Beat", "Chillwave", "Chiptune", "Dance-pop",
		"Dark Ambient", "Darkwave", "Deconstructed Club", "Deep House", "Deep Techno", "Disco",
		"Disco Polo", "Donk", "Downtempo", "Drone", "Drum n Bass", "Dub", "Dub Techno",
		"Dubstep", "Dungeon Synth", "EBM", "Electro", "Electro House", "Electro Swing",
		"Electroacoustic", "Electroclash", "Ethereal", "Euro House", "Euro-Disco", "Eurobeat",
		"Eurodance", "Experimental", "Footwork", "Freestyle", "French House", "Funkot",
		"Future Jazz", "Future Pop", "Gabber", "Garage House", "Ghetto", "Ghetto House",
		"Glitch", "Glitch Hop", "Goa Trance", "Gqom", "Grime", "Halftime", "Hands Up",
		"Happy Hardcore", "Hard House", "Hard Techno", "Hard Trance", "Hardcore", "Hardstyle",
		"Hi NRG", "Hip-House", "House", "Hyperpop", "IDM", "Illbient", "Industrial",
		"Italo House", "Italo-Disco", "Italodance", "Jazzdance", "Juke", "Jumpstyle", "Jungle",
		"Latin", "Leftfield", "Makina", "Minimal", "Minimal Techno", "Modern Classical",
		"Musique Concrète", "NDW", "Neofolk", "New Age", "New Beat", "New Wave", "Noise",
		"Nu-Disco", "Plunderphonics", "Power Electronics", "Progressive Breaks",
		"Progressive House", "Progressive Trance", "Psy-Trance", "Rhythmic Noise", "Schranz",
		"Sound Art", "Sound Collage", "Space-Age", "Speed Garage", "Speedcore", "Synth-pop",
		"Synthpunk", "Synthwave", "Tech House", "Tech Trance", "Techno", "Trance", "Tribal",
		"Tribal House", "Trip Hop", "Tropical House", "UK Funky", "UK Garage", "Vaporwave",
		"Video Game Music",
	}},
	{"Jazz", []string{
		"Afro-Cuban", "Afro-Cuban Jazz", "Afrobeat", "Avant-garde Jazz", "Big Band", "Bop",
		"Bossa Nova", "Contemporary Jazz", "Cool Jazz", "Dark Jazz", "Dixieland",
		"Easy Listening", "Exotica", "Free Funk", "Free Improvisation", "Free Jazz", "Fusion",
		"Gypsy Jazz", "Hard Bop", "Jazz-Funk", "Jazz-Rock", "Latin Jazz", "Lounge", "Modal",
		"Post Bop", "Ragtime", "Smooth Jazz", "Soul-Jazz", "Spiritual Jazz", "Swing",
	}},
	{"Funk / Soul", []string{
		"Afrobeat", "Bayou Funk", "Boogie", "Contemporary R&B", "Disco", "Free Funk", "Funk",
		"Go-Go", "Gospel", "Minneapolis Sound", "Neo Soul", "New Jack Swing", "P.Funk",
		"Psychedelic", "Rhythm & Blues", "Soul", "Swingbeat", "UK Street Soul",
	}},
	{"Hip Hop", []string{
		"Bass Music", "Beatbox", "Boom Bap", "Bounce", "Britcore", "Cloud Rap", "Conscious",
		"Crunk", "Cut-up/DJ", "DJ Battle Tool", "Drill", "Electro", "G-Funk", "Gangsta", "Grime",
		"Hardcore Hip-Hop", "Horrorcore", "Instrumental", "Jazzy Hip-Hop", "Miami Bass",
		"Pop Rap", "Ragga HipHop", "RnB/Swing", "Screw", "Thug Rap", "Trap", "Trip Hop",
		"Turntablism",
	}},
	{"Reggae", []string{
		"Calypso", "Dancehall", "Dub", "Dub Poetry", "Lovers Rock", "Mento", "Ragga", "Reggae",
		"Reggae Gospel", "Reggae-Pop", "Rocksteady", "Roots Reggae", "Ska", "Soca", "Toasting",
	}},
	{"Latin", []string{
		"Aguinaldo", "Axé", "Bachata", "Baião", "Batucada", "Beguine", "Bolero", "Bomba",
		"Boogaloo", "Bossanova", "Cha-Cha", "Champeta", "Charanga", "Compas", "Conjunto",
		"Cubano", "Cumbia", "Danzon", "Descarga", "Forró", "Funaná", "Guaguancó", "Guajira",
		"Guaracha", "Jibaro", "Lambada", "Mambo", "Mariachi", "Merengue", "MPB",
		"Música Criolla", "Norteño", "Nueva Cancion", "Nueva Trova", "Pachanga", "Plena",
		"Porro", "Ranchera", "Reggaeton", "Rumba", "Salsa", "Samba", "Son", "Son Montuno",
		"Tango", "Tejano", "Timba", "Trova", "Vallenato",
	}},
	{"Rock", []string{
		"Acid Rock", "Acoustic", "Alternative Metal", "Alternative Rock", "Antifolk", "AOR",
		"Arena Rock", "Art Rock", "Black Metal", "Blues Rock", "Britpop", "Classic Rock",
		"Coldwave", "Country Rock", "Crust", "Death Metal", "Doom Metal", "Dream Pop", "Emo",
		"Experimental", "Folk Metal", "Folk Rock", "Funk Metal", "Garage Rock", "Glam",
		"Goth Rock", "Gothic Metal", "Grindcore", "Groove Metal", "Grunge", "Hard Rock",
		"Hardcore", "Heavy Metal", "Horror Rock", "Indie Rock", "Industrial", "Industrial Metal",
		"J-Rock", "Krautrock", "Lo-Fi", "Math Rock", "Melodic Hardcore", "Metalcore", "Mod",
		"No Wave", "Noise", "Noise Rock", "Nu Metal", "Oi", "Pop Punk", "Pop Rock", "Post Rock",
		"Post-Hardcore", "Post-Punk", "Power Pop", "Prog Rock", "Progressive Metal",
		"Psychedelic Rock", "Pub Rock", "Punk", "Rock & Roll", "Rock Opera", "Rockabilly",
		"Shoegaze", "Ska", "Slowcore", "Sludge Metal", "Soft Rock", "Southern Rock",
		"Space Rock", "Speed Metal", "Stoner Rock", "Surf", "Symphonic Rock", "Thrash",
	}},
	{"Pop", []string{
		"Alt-Pop", "Anison", "Ballad", "Baroque Pop", "Beat", "Bollywood", "Bubblegum",
		"Chanson", "City Pop", "Doo Wop", "Ethno-pop", "Europop", "Indie Pop", "Indo-Pop",
		"J-pop", "Jangle Pop", "K-pop", "Kayōkyoku", "Latin Pop", "Light Music", "Music Hall",
		"Novelty", "Schlager", "Shibuya-Kei", "Sunshine Pop", "Twist", "Vocal", "Yé-Yé",
	}},
	{"Folk, World, & Country", []string{
		"Aboriginal", "African", "Bluegrass", "Cajun", "Cambodian Classical",
		"Canzone Napoletana", "Celtic", "Chutney", "Country", "Fado", "Flamenco", "Folk",
		"Gamelan", "Gwo Ka", "Highlife", "Hillbilly", "Hindustani", "Honky Tonk",
		"Indian Classical", "Junkanoo", "Keroncong", "Klezmer", "Kolo", "Laïkó", "Luk Thung",
		"Mbalax", "Min'yō", "Mo Lam", "Mouth Music", "Nhạc Vàng", "Nordic", "Pacific", "Polka",
		"Progressive Bluegrass", "Raï", "Rebetiko", "Romani", "Sea Shanties", "Skiffle",
		"Soukous", "Steel Band", "Volksmusik", "Western Swing", "Zouk", "Zydeco",
	}},
	{"Blues", []string{
		"Boogie Woogie", "Chicago Blues", "Country Blues", "Delta Blues", "East Coast Blues",
		"Electric Blues", "Harmonica Blues", "Hill Country Blues", "Jump Blues",
		"Louisiana Blues", "Memphis Blues", "Modern Electric Blues", "Piano Blues",
		"Texas Blues",
	}},
	{"Classical", []string{
		"Atonal", "Baroque", "Cantata", "Chamber Music", "Choral", "Concerto", "Concerto Grosso",
		"Contemporary", "Etude", "Gagaku", "Impressionist", "Keyboard", "Late Romantic",
		"Medieval", "Minimalism", "Modern", "Neo-Classical", "Neo-Romantic", "Opera", "Operetta",
		"Organ", "Piano", "Plainchant", "Post-Modern", "Prelude", "Program Music", "Renaissance",
		"Romantic", "Serial", "Shomyo", "Solo", "Sonata", "String Ensemble", "String Instrument",
		"Suite", "Symphony", "Theme With Variations", "Tone Poem", "Wind Ensemble",
	}},
	{"Stage & Screen", []string{
		"Cabaret", "Concert Film", "Music Video", "Musical", "Score", "Soundtrack", "Theme",
		"Vaudeville",
	}},
	{"Non-Music", []string{
		"Audiobook", "Comedy", "Dialogue", "Education", "Erotic", "Field Recording",
		"Health-Fitness", "Holiday", "Interview", "Medical", "Monolog", "Movie Effects",
		"Occult", "Parody", "Poetry", "Political", "Promotional", "Public Broadcast",
		"Public Service Announcement", "Radioplay", "Religious", "Sermon", "Special Effects",
		"Speech", "Spoken Word", "Sport", "Story", "Technical", "Therapy",
	}},
	{"Children's", []string{
		"Educational", "Lullaby", "Nursery Rhymes", "Story",
	}},
	{"Brass & Military", []string{
		"Ballroom", "Barbershop", "Brass Band", "Concert Band", "Guggenmusik", "Marches",
		"Military", "Orchestra", "Pipe & Drum",
	}},
}
