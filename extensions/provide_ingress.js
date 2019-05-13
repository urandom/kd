(function() {
    function Ingress() {
        this.namespace = "";
        this.names = [];
    }

    Ingress.prototype.controllerFactory = function(obj, podTree) {
        var svcName = obj.Spec.Backend.ServiceName
        var selector = {}
        podTree.Controllers.forEach(function(controller) {
            if (controller.Category() == "Service" && controller.GetObjectMeta().GetName() == svcName) {
                svcSelector = controller.Selector()
                Object.keys(svcSelector).forEach(function(key) {
                    selector[key] = svcSelector[key]
                }.bind(this)) 
            }
        }.bind(this))
        return GenericCtrl(obj, "Ingress", selector, podTree)
    }

    Ingress.prototype.list = function(c, ns, opts) {
        var list = c.ExtensionsV1beta1().Ingresses(ns).List(opts)
        return function(tree) {
            controllers = []

            list.Items.forEach(function(item) {
                controllers.push(this.controllerFactory(ptr(item), tree))
            }.bind(this))

            return controllers
        }.bind(this)
    }

    Ingress.prototype.watch = function(c, ns, opts) {
        return c.ExtensionsV1beta1().Ingresses(ns).Watch(opts)
    }

    Ingress.prototype.update = function(c, obj) {
        c.ExtensionsV1beta1().Ingresses(obj.Namespace).Update(obj)
    }

    Ingress.prototype.del = function(c, obj, opts) {
        c.ExtensionsV1beta1().Ingresses(obj.Namespace).Delete(obj.Name, opts)
    }

    Ingress.prototype.summary = function(obj) {
        return sprintf("[skyblue::b]Data:[white::-] %d\n", Object.keys(obj.Data).length)
    }

    var ingress = new Ingress()

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectSummaryProvider("Ingress", ingress.summary.bind(ingress))

    // Register callback to provide a controller-wrapper of a certain type
    kd.RegisterControllerOperator("Ingress", {
        "Factory": ingress.controllerFactory.bind(ingress),
        "List": ingress.list.bind(ingress),
        "Watch": ingress.watch.bind(ingress),
        "Update": ingress.update.bind(ingress),
        "Delete": ingress.del.bind(ingress)
    })
})()
