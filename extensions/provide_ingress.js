(function() {
    function Ingress() {
    }

    Ingress.prototype.controllerFactory = function(obj, podTree) {
        var selector = {}
        var svcNames = []
        if (obj.Spec.Backend) {
            svcNames.push(obj.Spec.Backend.ServiceName)
        }
        obj.Spec.Rules.forEach(function(rule) {
            if (rule.HTTP) {
                rule.HTTP.Paths.forEach(function(path) {
                    svcNames.push(path.Backend.ServiceName)
                })
            }
        })
        podTree.Controllers.forEach(function(controller) {
            if (controller.Category() == "Service" && svcNames.indexOf(controller.GetObjectMeta().GetName()) != -1) {
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
        var summary = sprintf("[skyblue::b]Rules:[white::-] %d\n", Object.keys(obj.Spec.Rules).length)
        var hosts = []
        var paths = []
        var ports = []

        obj.Spec.Rules.forEach(function(rule) {
            hosts.push(rule.Host)
            if (rule.HTTP) {
                rule.HTTP.Paths.forEach(function(path) {
                    paths.push(path.Path)
                    if (path.Backend.ServicePort.StrVal != "") {
                        ports.push(path.Backend.ServicePort.StrVal)
                    } else if (path.Backend.ServicePort.IntVal > 0) {
                        ports.push(path.Backend.ServicePort.IntVal)
                    }
                })
            }
        })
        if (hosts.length) {
            summary += sprintf("[skyblue::b]Hosts:[white::-] %s\n", hosts.join(", "))
        }
        if (paths.length) {
            summary += sprintf("[skyblue::b]Paths:[white::-] %s\n", paths.join(", "))
        }
        if (ports.length) {
            summary += sprintf("[skyblue::b]Ports:[white::-] %s\n", ports.join(", "))
        }

        return summary
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
