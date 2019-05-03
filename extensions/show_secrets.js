(function() {
    function Secrets() {
        this.namespace = "";
        this.names = [];
        this.secrets = [];
    }

    Secrets.prototype.onObjectSelected = function(obj) {
        if (obj.Kind == "Pod") {
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
        } else if (obj.Kind == "Secret") {
            this.secrets = obj.Data

            if (!Object.keys(this.secrets).length) {
                return null
            }

            return {"label": "Secret data", "cb": this.secretKeysActionCallback.bind(this)}
        }

        return null
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

    Secrets.prototype.secretKeysActionCallback = function() {
        var keys = Object.keys(this.secrets)
        var key = keys[0];

        if (keys.length > 1) {
            key = kd.Choose("Secret keys", keys)
        }

        // Display can show plain text, or a k8s object
        kd.Display(this.secrets[key])
    }

    var Secrets = new Secrets()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(Secrets.onObjectSelected.bind(Secrets))
})()
