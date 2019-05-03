(function() {
    function Secrets() {
        this.namespace = "";
        this.names = [];
    }

    Secrets.prototype.onObjectSelected = function(obj) {
        if (obj.Kind != "Pod") {
            return null
        }
        this.names = [];
        obj.Spec.Volumes.forEach(function(vol) {
            if (vol.Secret != null) {
                this.names.push(vol.Secret.SecretName)
            }
        }.bind(this))

        if (!this.names.length) {
            return null
        }

        this.namespace = obj.Namespace

        return {"label": "Secret", "cb": this.actionCallback.bind(this)}
    }

    Secrets.prototype.actionCallback = function() {
        var name = this.names[0];

        if (this.names.length > 1) {
            // Displays a list dialog with the string array, returning the
            // user-selected one
            name = kd.Choose("Secrets", this.names)
        }

        // Client holds the k8s.Client object
        var secret = kd.Client().CoreV1().Secrets(this.namespace).Get(name, {})

        // Display can show plain text, or a k8s object
        kd.Display(secret)
    }

    var Secrets = new Secrets()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(Secrets.onObjectSelected.bind(Secrets))
})()
