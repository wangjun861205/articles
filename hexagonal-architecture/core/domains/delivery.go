package domains

type Shipping struct {
	ID        int
	Name      string
	Address   string
	Phone     string
	ProductID string
}

type ShippingInsert struct {
	Name      string
	Address   string
	Phone     string
	ProductID int
}
