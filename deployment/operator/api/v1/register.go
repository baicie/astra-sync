package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

func SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&SyncJob{}).
		Owns(&Connection{}).
		Complete(&SyncJobReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		})
}

var (
	GroupVersionKind = GroupVersion.WithKind("SyncJob")
	GroupVersion     = SchemeGroupVersion
)

func init() {
	SchemeBuilder.Register(&SyncJob{}, &SyncJobList{})
	SchemeBuilder.Register(&Connection{}, &ConnectionList{})
}

var SchemeBuilder = runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
	return nil
})
