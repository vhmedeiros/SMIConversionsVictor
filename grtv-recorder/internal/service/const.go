package service

// ServiceName é o nome interno do serviço Windows (usado por sc.exe / mgr).
// Deve bater com scripts\install.bat e uninstall.bat (SPEC.md §10.2).
const ServiceName = "GRTVRecorder"

// ServiceDisplayName e ServiceDescription aparecem no services.msc (SPEC.md §10.2).
const (
	ServiceDisplayName = "GRTV Recorder"
	ServiceDescription = "Gravacao continua de 8 canais de TV em blocos de 4 minutos"
)
