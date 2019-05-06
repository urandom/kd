(function() {
    function ConfigMap() {
        this.namespace = "";
        this.names = [];
    }

    ConfigMap.prototype.onObjectSelected = function(obj) {
        if (sprintf("%T", obj).split(".")[1] != "Pod") {
            return null
        }
        this.names = [];
        obj.Spec.Volumes.forEach(function(vol) {
            if (vol.ConfigMap != null) {
                this.names.push(vol.ConfigMap.Name)
            }
        }.bind(this))

        if (!this.names.length) {
            return null
        }

        this.namespace = obj.Namespace

        return {"label": "Config map", "cb": this.actionCallback.bind(this)}
    }

    ConfigMap.prototype.actionCallback = function() {
        var name = this.names[0];

        if (this.names.length > 1) {
            // Displays a list dialog with the string array, returning the
            // user-selected one
            name = kd.PickFrom("Config maps", this.names)
        }

        // Client holds the k8s.Client object
        var config = kd.Client().CoreV1().ConfigMaps(this.namespace).Get(name, {})
        //
        // Display can show plain text, or a k8s object
        kd.Display(config)
    }

    ConfigMap.prototype.del = function(obj) {
        kd.Client().CoreV1().ConfigMaps(obj.Namespace).Delete(obj.Name, {"PropagationPolicy": "Foreground"})
    }

    ConfigMap.prototype.update = function(obj) {
        kd.Client().CoreV1().ConfigMaps(obj.Namespace).Update(obj, {})
    }

    ConfigMap.prototype.summary = function(obj) {
        return sprintf("[skyblue::b]Data:[white::-] %d\n", Object.keys(obj.Data).length)
    }

    var configMap = new ConfigMap()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(configMap.onObjectSelected.bind(configMap))

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectMutateActions("ConfigMap", {"delete": configMap.del.bind(configMap), "update": configMap.update.bind(configMap)})

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectSummaryProvider("ConfigMap", configMap.summary.bind(configMap))
})()
