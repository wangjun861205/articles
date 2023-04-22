package ports

import (
	"ecommerce/core/domains"
)

type DeliveryRepository interface {
	Create(domains.ShippingInsert) (int, error)
	Get(id int) (domains.Shipping, error)
}

type InventoryRepository interface {
	Get(id int) (domains.Inventory, error)
	Dec(id, num int) error
}

type UserRepository interface {
	Get(id int) (domains.User, error)
}
