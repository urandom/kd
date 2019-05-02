(function() {
    function ConfigMap() {
        this.namespace = "";
        this.names = [];
    }

    ConfigMap.prototype.onObjectSelected = function(obj) {
        if (obj.Kind != "Pod") {
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
            name = kd.Choose("Config maps", this.names)
        }

        // Client holds the k8s.Client object
        var config = kd.Client.CoreV1().ConfigMaps(this.namespace).Get(name, {})

        // Display show the text in the details pane
        kd.Display(kd.ToYAML(config))
    }

    var configMap = new ConfigMap()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(configMap.onObjectSelected.bind(configMap))
})()
