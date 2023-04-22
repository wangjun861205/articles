package core

import (
	"ecommerce/core/domains"
	"ecommerce/core/ports"
	"errors"
)

type OrderRequest struct {
	UID       int
	ProductID int
}

func Order(req OrderRequest, userRepo ports.UserRepository, inventoryRepo ports.InventoryRepository, delivery ports.DeliveryRepository) (int, error) {
	user, err := userRepo.Get(req.UID)
	if err != nil {
		return -1, err
	}
	inventory, err := inventoryRepo.Get(req.ProductID)
	if err != nil {
		return -1, err
	}
	if inventory.Total == 0 {
		return -1, errors.New("库存不足")
	}
	return delivery.Create(domains.ShippingInsert{Name: user.Name, Address: user.Address, Phone: user.Phone, ProductID: req.ProductID})
}
