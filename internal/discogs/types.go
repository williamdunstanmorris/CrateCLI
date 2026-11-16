package discogs

type Pagination struct {
	Page    int `json:"page"`
	Pages   int `json:"pages"`
	PerPage int `json:"per_page"`
	Items   int `json:"items"`
}

// SearchResult is one hit from /database/search.
type SearchResult struct {
	ID         int      `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"` // "Artist - Title"
	Year       string   `json:"year"`
	Country    string   `json:"country"`
	Format     []string `json:"format"`
	Label      []string `json:"label"`
	CatNo      string   `json:"catno"`
	Genre      []string `json:"genre"`
	Style      []string `json:"style"`
	URI        string   `json:"uri"`
	CoverImage string   `json:"cover_image"`
	Thumb      string   `json:"thumb"`
	MasterID   int      `json:"master_id"`
	Community  struct {
		Want int `json:"want"`
		Have int `json:"have"`
	} `json:"community"`
}

type SearchResponse struct {
	Pagination Pagination     `json:"pagination"`
	Results    []SearchResult `json:"results"`
}

type Price struct {
	Value    float64 `json:"value"`
	Currency string  `json:"currency"`
}

// Listing is one marketplace item from a seller's inventory.
type Listing struct {
	ID        int    `json:"id"`
	URI       string `json:"uri"`
	Status    string `json:"status"`
	Condition string `json:"condition"`
	Sleeve    string `json:"sleeve_condition"`
	Comments  string `json:"comments"`
	ShipsFrom string `json:"ships_from"`
	Posted    string `json:"posted"`
	Price     Price  `json:"price"`
	Release   struct {
		ID          int    `json:"id"`
		Artist      string `json:"artist"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Format      string `json:"format"`
		Year        int    `json:"year"`
		Label       string `json:"label"`
		CatNo       string `json:"catalog_number"`
		Thumbnail   string `json:"thumbnail"`
		Stats       struct {
			Community struct {
				InWantlist   int `json:"in_wantlist"`
				InCollection int `json:"in_collection"`
			} `json:"community"`
		} `json:"stats"`
	} `json:"release"`
	Seller struct {
		Username string `json:"username"`
	} `json:"seller"`
}

type InventoryResponse struct {
	Pagination Pagination `json:"pagination"`
	Listings   []Listing  `json:"listings"`
}

// Want is one entry in a user's public wantlist.
type Want struct {
	ID               int `json:"id"`
	BasicInformation struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Year    int    `json:"year"`
		Thumb   string `json:"thumb"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Labels []struct {
			Name  string `json:"name"`
			CatNo string `json:"catno"`
		} `json:"labels"`
		Formats []struct {
			Name         string   `json:"name"`
			Descriptions []string `json:"descriptions"`
		} `json:"formats"`
		Genres []string `json:"genres"`
		Styles []string `json:"styles"`
	} `json:"basic_information"`
}

type WantsResponse struct {
	Pagination Pagination `json:"pagination"`
	Wants      []Want     `json:"wants"`
}

type Video struct {
	URI      string `json:"uri"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

// Release is the subset of /releases/{id} that cratecli needs.
type Release struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	URI     string `json:"uri"`
	Country string `json:"country"`
	Artists []struct {
		Name string `json:"name"`
		Join string `json:"join"`
	} `json:"artists"`
	Labels []struct {
		Name  string `json:"name"`
		CatNo string `json:"catno"`
	} `json:"labels"`
	Formats []struct {
		Name         string   `json:"name"`
		Descriptions []string `json:"descriptions"`
	} `json:"formats"`
	Genres    []string `json:"genres"`
	Styles    []string `json:"styles"`
	Videos    []Video  `json:"videos"`
	Tracklist []struct {
		Position string `json:"position"`
		Title    string `json:"title"`
		Duration string `json:"duration"`
	} `json:"tracklist"`
	Community struct {
		Want int `json:"want"`
		Have int `json:"have"`
	} `json:"community"`
	LowestPrice float64 `json:"lowest_price"`
	NumForSale  int     `json:"num_for_sale"`
}
