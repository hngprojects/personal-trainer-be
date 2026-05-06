.PHONY: help run build test test-cover lint fmt tidy clean \
        install-tools sqlc \
        migrate-up migrate-down migrate-create \
        migrate-version migrate-force migrate-drop

codegen:
	oapi-codegen -config oapi-codegen.yaml api.yaml